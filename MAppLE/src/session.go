package quic

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/lucas-clemente/quic-go/ackhandler"
	"github.com/lucas-clemente/quic-go/congestion"
	"github.com/lucas-clemente/quic-go/fec"
	"github.com/lucas-clemente/quic-go/internal/flowcontrol"
	"github.com/lucas-clemente/quic-go/internal/handshake"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
	"github.com/lucas-clemente/quic-go/internal/wire"
	"github.com/lucas-clemente/quic-go/logger"
	"github.com/lucas-clemente/quic-go/qerr"
)

type unpacker interface {
	Unpack(headerBinary []byte, hdr *wire.Header, data []byte, recovered bool) (*unpackedPacket, error)
}

type receivedPacket struct {
	remoteAddr net.Addr
	header     *wire.Header
	data       []byte
	rcvTime    time.Time
	rcvPconn   net.PacketConn
	recovered  bool
}

var (
	errRstStreamOnInvalidStream   = errors.New("RST_STREAM received for unknown stream")
	errWindowUpdateOnClosedStream = errors.New("WINDOW_UPDATE received for an already closed stream")
)

var (
	newCryptoSetup       = handshake.NewCryptoSetup
	newCryptoSetupClient = handshake.NewCryptoSetupClient
)

type handshakeEvent struct {
	encLevel protocol.EncryptionLevel
	err      error
}

type closeError struct {
	err    error
	remote bool
}

type sessionI interface {
	Session

	GetConfig() *Config
	GetConnectionID() protocol.ConnectionID
	GetCryptoSetup() handshake.CryptoSetup
	GetMaxPathID() protocol.PathID
	GetPacker() *packetPacker
	GetPeerBlocked() bool
	GetPerspective() protocol.Perspective
	GetStreamFramer() *streamFramer
	GetFECFramer() *FECFramer
	GetUnpacker() unpacker
	GetVersion() protocol.VersionNumber
	GetFECFrameworkReceiver() *FECFrameworkReceiver
	GetFECFrameworkConvolutionalReceiver() *FECFrameworkReceiverConvolutional
	GetFECFrameworkSender() *FECFrameworkSender
	getWindowUpdates(force bool) []wire.Frame
	IsHandshakeComplete() bool
	PathManager() *pathManager
	Paths() map[protocol.PathID]*path
	PathsLock() *sync.RWMutex
	PathTimersChan() chan *path
	SchedulePathsFrame()
	sendPackedPacket(packet *packedPacket, pth *path) error
	SendPing(pth *path) error
	SetPeerBlocked(peerBlocked bool)
	onHasFECData()

	GetLargestRcvdPacketNumber() protocol.PacketNumber
	MaybeSetLargestRcvdPacketNumber(protocol.PacketNumber)
}

// A Session is a QUIC session
type session struct {
	connectionID protocol.ConnectionID
	perspective  protocol.Perspective
	version      protocol.VersionNumber
	config       *Config

	paths     map[protocol.PathID]*path
	pathsLock sync.RWMutex
	maxPathID protocol.PathID

	streamsMap   *streamsMap
	cryptoStream streamI

	rttStats *congestion.RTTStats

	remoteRTTs         map[protocol.PathID]time.Duration
	lastPathsFrameSent time.Time

	streamFramer *streamFramer

	// added by michelfra: fecFramer
	fecFramer *FECFramer

	connFlowController flowcontrol.ConnectionFlowController

	unpacker unpacker
	packer   *packetPacker

	peerBlocked bool

	cryptoSetup handshake.CryptoSetup

	receivedPackets  chan *receivedPacket
	recoveredPackets chan *receivedPacket
	sendingScheduled chan struct{}
	fecScheduled     chan struct{}
	// closeChan is used to notify the run loop that it should terminate.
	closeChan chan closeError
	closeOnce sync.Once

	ctx       context.Context
	ctxCancel context.CancelFunc

	// when we receive too many undecryptable packets during the handshake, we send a Public reset
	// but only after a time of protocol.PublicResetTimeout has passed
	undecryptablePackets                   []*receivedPacket
	receivedTooManyUndecrytablePacketsTime time.Time

	// this channel is passed to the CryptoSetup and receives the transport parameters, as soon as the peer sends them
	paramsChan <-chan handshake.TransportParameters
	// this channel is passed to the CryptoSetup and receives the current encryption level
	// it is closed as soon as the handshake is complete
	aeadChanged       <-chan protocol.EncryptionLevel
	handshakeComplete bool
	// will be closed as soon as the handshake completes, and receive any error that might occur until then
	// it is used to block WaitUntilHandshakeComplete()
	handshakeCompleteChan chan error
	// handshakeChan receives handshake events and is closed as soon the handshake completes
	// the receiving end of this channel is passed to the creator of the session
	// it receives at most 3 handshake events: 2 when the encryption level changes, and one error
	handshakeChan chan<- handshakeEvent

	sessionCreationTime     time.Time
	lastNetworkActivityTime time.Time

	peerParams *handshake.TransportParameters

	timer *utils.Timer
	// keepAlivePingSent stores whether a Ping frame was sent to the peer or not
	// it is reset as soon as we receive a packet from the peer
	keepAlivePingSent bool

	pathTimers chan *path

	pathManager         *pathManager
	pathManagerLaunched bool

	scheduler *scheduler

	// added by michelfra:
	fecFrameworkReceiver              *FECFrameworkReceiver
	fecFrameworkReceiverConvolutional *FECFrameworkReceiverConvolutional
	fecFrameworkSender                *FECFrameworkSender
	fecScheduler                      fec.FECScheduler
	receiverFECScheme                 fec.FECScheme
	senderFECScheme                   fec.FECScheme
	redundancyController              fec.RedundancyController
	ReceivedFECFrames                 []*wire.FECFrame //Received FEC frames not already handled
	nRetransmissions                  uint64
	bulkRecovery                      bool

	largestRcvdPacketNumber protocol.PacketNumber
}

var _ Session = &session{}
var _ sessionI = &session{}

// newSession makes a new session
func newSession(
	conn connection,
	pconnMgr pconnManagerI,
	v protocol.VersionNumber,
	connectionID protocol.ConnectionID,
	sCfg *handshake.ServerConfig,
	tlsConf *tls.Config,
	config *Config,
) (packetHandler, <-chan handshakeEvent, error) {
	fs, err := GetFECSchemeFromID(config.FECScheme)
	if err != nil {
		return nil, nil, err
	}
	s := &session{
		paths:             make(map[protocol.PathID]*path),
		remoteRTTs:        make(map[protocol.PathID]time.Duration),
		connectionID:      connectionID,
		perspective:       protocol.PerspectiveServer,
		version:           v,
		config:            config,
		receiverFECScheme: fs,
	}
	return s.setup(sCfg, "", tlsConf, v, nil, conn, pconnMgr)
}

// declare this as a variable, such that we can it mock it in the tests
var newClientSession = func(
	conn connection,
	pconnMgr pconnManagerI,
	hostname string,
	v protocol.VersionNumber,
	connectionID protocol.ConnectionID,
	tlsConf *tls.Config,
	config *Config,
	initialVersion protocol.VersionNumber,
	negotiatedVersions []protocol.VersionNumber, // needed for validation of the GQUIC version negotiaton
) (packetHandler, <-chan handshakeEvent, error) {
	fs, err := GetFECSchemeFromID(config.FECScheme)
	if err != nil {
		return nil, nil, err
	}
	s := &session{
		paths:             make(map[protocol.PathID]*path),
		remoteRTTs:        make(map[protocol.PathID]time.Duration),
		connectionID:      connectionID,
		perspective:       protocol.PerspectiveClient,
		version:           v,
		config:            config,
		receiverFECScheme: fs,
	}
	println("client FEC SCHEME: ", fs)
	return s.setup(nil, hostname, tlsConf, initialVersion, negotiatedVersions, conn, pconnMgr)
}

func (s *session) setup(
	scfg *handshake.ServerConfig,
	hostname string,
	tlsConf *tls.Config,
	initialVersion protocol.VersionNumber,
	negotiatedVersions []protocol.VersionNumber,
	conn connection,
	pconnMgr pconnManagerI,
) (packetHandler, <-chan handshakeEvent, error) {
	aeadChanged := make(chan protocol.EncryptionLevel, 2)
	paramsChan := make(chan handshake.TransportParameters)
	s.aeadChanged = aeadChanged
	s.paramsChan = paramsChan
	handshakeChan := make(chan handshakeEvent, 3)
	s.handshakeChan = handshakeChan
	s.handshakeCompleteChan = make(chan error, 1)
	s.receivedPackets = make(chan *receivedPacket, protocol.MaxSessionUnprocessedPackets)
	s.recoveredPackets = make(chan *receivedPacket, protocol.MaxSessionUnprocessedPackets)
	s.closeChan = make(chan closeError, 1)
	s.sendingScheduled = make(chan struct{}, 1)
	s.fecScheduled = make(chan struct{}, 1)
	s.undecryptablePackets = make([]*receivedPacket, 0, protocol.MaxUndecryptablePackets)
	s.ctx, s.ctxCancel = context.WithCancel(context.Background())

	s.timer = utils.NewTimer()
	now := time.Now()
	s.lastNetworkActivityTime = now
	s.sessionCreationTime = now

	transportParams := &handshake.TransportParameters{
		StreamFlowControlWindow:     protocol.ReceiveStreamFlowControlWindow,
		ConnectionFlowControlWindow: protocol.ReceiveConnectionFlowControlWindow,
		MaxStreams:                  protocol.MaxIncomingStreams,
		IdleTimeout:                 s.config.IdleTimeout,
		CacheHandshake:              s.config.CacheHandshake,
		MaxPathID:                   protocol.PathID(s.config.MaxPathID),
		FECScheme:                   s.config.FECScheme,
	}
	s.scheduler = &scheduler{redundancyController: s.redundancyController}
	s.scheduler.setup()

	// s.redundancyController = fec.NewAverageRedundancyController()
	if s.config.RedundancyController == nil {
		s.redundancyController = fec.NewConstantRedundancyController(uint(protocol.NumberOfFecPackets), uint(protocol.NumberOfRepairSymbols), uint(protocol.NumberOfInterleavedFECGroups), uint(protocol.ConvolutionalStepSize))
	} else {
		s.redundancyController = s.config.RedundancyController
	}

	if pconnMgr == nil && conn != nil {
		// XXX ONLY VALID FOR BENCHMARK!
		s.paths[protocol.InitialPathID] = &path{
			pathID: protocol.InitialPathID,
			sess:   s,
			conn:   conn,
		}
		s.paths[protocol.InitialPathID].setup(nil, s.redundancyController)
	} else if pconnMgr != nil && conn != nil {
		s.pathManager = &pathManager{pconnMgr: pconnMgr, sess: s}
		s.pathManager.setup(conn, s.redundancyController)
	} else {
		panic("session without conn")
	}
	// XXX (QDC): use the PathID 0 as the session RTT path
	s.rttStats = s.paths[protocol.InitialPathID].rttStats
	s.connFlowController = flowcontrol.NewConnectionFlowController(
		protocol.ReceiveConnectionFlowControlWindow,
		protocol.ByteCount(s.config.MaxReceiveConnectionFlowControlWindow),
		s.rttStats,
		s.remoteRTTs,
	)
	s.streamsMap = newStreamsMap(s.newStream, s.perspective, s.version)
	s.cryptoStream = s.newStream(s.version.CryptoStreamID())
	s.streamFramer = newStreamFramer(s.cryptoStream, s.streamsMap, s.connFlowController, s.config.ProtectReliableStreamFrames)

	s.fecFramer = newFECFramer(s, s.version)
	s.fecScheduler = fec.NewRoundRobinScheduler(s.redundancyController, s.version)

	s.pathTimers = make(chan *path)

	var err error
	if s.perspective == protocol.PerspectiveServer {
		verifySourceAddr := func(clientAddr net.Addr, cookie *Cookie) bool {
			return s.config.AcceptCookie(clientAddr, cookie)
		}
		if s.version.UsesTLS() {
			s.cryptoSetup, err = handshake.NewCryptoSetupTLSServer(
				s.cryptoStream,
				s.connectionID,
				tlsConf,
				s.paths[protocol.InitialPathID].conn.RemoteAddr(),
				transportParams,
				paramsChan,
				aeadChanged,
				verifySourceAddr,
				s.config.Versions,
				s.version,
			)
		} else {
			s.cryptoSetup, err = newCryptoSetup(
				s.cryptoStream,
				s.connectionID,
				s.paths[protocol.InitialPathID].conn.RemoteAddr(),
				s.version,
				scfg,
				transportParams,
				s.config.Versions,
				verifySourceAddr,
				paramsChan,
				aeadChanged,
			)
		}
	} else {
		transportParams.OmitConnectionID = s.config.RequestConnectionIDOmission
		if s.version.UsesTLS() {
			s.cryptoSetup, err = handshake.NewCryptoSetupTLSClient(
				s.cryptoStream,
				s.connectionID,
				hostname,
				tlsConf,
				transportParams,
				paramsChan,
				aeadChanged,
				initialVersion,
				s.config.Versions,
				s.version,
			)
		} else {
			s.cryptoSetup, err = newCryptoSetupClient(
				s.cryptoStream,
				hostname,
				s.connectionID,
				s.version,
				tlsConf,
				transportParams,
				paramsChan,
				aeadChanged,
				initialVersion,
				negotiatedVersions,
			)
		}
	}
	if err != nil {
		return nil, nil, err
	}

	s.packer = newPacketPacker(s.connectionID,
		s.cryptoSetup,
		s.streamFramer,
		s.perspective,
		s.version,
		s.fecFramer,
		s,
	)
	s.unpacker = &packetUnpacker{aead: s.cryptoSetup, version: s.version, sess: s}

	if f, ok := s.receiverFECScheme.(fec.BlockFECScheme); ok {
		s.fecFrameworkReceiver = NewFECFrameworkReceiver(s, f)
	} else {
		// TODO: use interface to not have two different types
		s.fecFrameworkReceiverConvolutional = NewFECFrameworkReceiverConvolutional(s, s.receiverFECScheme.(fec.ConvolutionalFECScheme))
	}
	s.fecFrameworkSender = NewFECFrameworkSender(s.senderFECScheme, s.fecScheduler, s.fecFramer, s.redundancyController, s.version)
	s.bulkRecovery = true

	return s, handshakeChan, nil
}

// run the session main loop
func (s *session) run() error {
	defer s.ctxCancel()

	go func() {
		if err := s.cryptoSetup.HandleCryptoStream(); err != nil {
			s.Close(err)
		}
	}()

	var closeErr closeError
	aeadChanged := s.aeadChanged

	var timerPth *path

	NRecoveredPackets := 0

runLoop:
	for {
		// close connection after one second of inactivity
		// FIXME ugly, time is normally defined in QUIC handshake
		// if s.lastNetworkActivityTime.Before(time.Now().Add(-time.Second)) {
		// s.closePaths()
		// break runLoop
		// }

		// Close immediately if requested
		select {
		case closeErr = <-s.closeChan:
			s.closePaths()
			break runLoop
		default:
		}

		s.maybeResetTimer()

		select {
		case closeErr = <-s.closeChan:
			// We stop running the path manager, which will close paths
			s.closePaths()
			break runLoop
		case p := <-s.recoveredPackets:
			var paths []protocol.PathID
			n := 1
			p.recovered = true
			NRecoveredPackets++
			paths = append(paths, p.header.PathID)
			err := s.handlePacketImpl(p)
			if err != nil {
				if qErr, ok := err.(*qerr.QuicError); ok && qErr.ErrorCode == qerr.DecryptionFailure {
					s.tryQueueingUndecryptablePacket(p)
					continue
				}
				s.closeLocal(err)
				continue
			}

		recoverPacketsLoop:
			for n < protocol.MAX_RECOVERED_PACKETS_IN_ONE_ROW {
				// handle all the currently recovered packets
				select {
				case p := <-s.recoveredPackets:
					p.recovered = true
					n++
					NRecoveredPackets++
					paths = append(paths, p.header.PathID)
					err := s.handlePacketImpl(p)
					if err != nil {
						if qErr, ok := err.(*qerr.QuicError); ok && qErr.ErrorCode == qerr.DecryptionFailure {
							s.tryQueueingUndecryptablePacket(p)
							continue
						} else {
						}
						s.closeLocal(err)
						continue
					}
				default:
					if n > 0 {
					}
					break recoverPacketsLoop
				}
			}
			/*
				for _, pthID := range paths {
					pth, ok := s.paths[pthID]
					if ok {
						f := pth.receivedPacketHandler.GetRecoveredFrame()
						pth.receivedPacketHandler.SentRecoveredFrame(f) // clean the recovered history
						if f != nil {
							s.packer.QueueControlFrame(f, pth)
						}
					}
				}*/
		case <-s.timer.Chan():
			s.timer.SetRead()
			// We do all the interesting stuff after the switch statement, so
			// nothing to see here.
		case <-s.fecScheduled:
			// We do all the interesting stuff after the switch statement, so
			// nothing to see here.
		case <-s.sendingScheduled:
			// We do all the interesting stuff after the switch statement, so
			// nothing to see here.
		case tmpPth := <-s.pathTimers:
			timerPth = tmpPth
			// We do all the interesting stuff after the switch statement, so
			// nothing to see here.
		case p := <-s.receivedPackets:
			err := s.handlePacketImpl(p)
			if err != nil {
				if qErr, ok := err.(*qerr.QuicError); ok && qErr.ErrorCode == qerr.DecryptionFailure {
					s.tryQueueingUndecryptablePacket(p)
					continue
				}
				s.closeLocal(err)
				continue
			}
			// This is a bit unclean, but works properly, since the packet always
			// begins with the public header and we never copy it.
			putPacketBuffer(p.header.Raw)
		case p := <-s.paramsChan:
			s.processTransportParameters(&p)
		case l, ok := <-aeadChanged:
			if !ok { // the aeadChanged chan was closed. This means that the handshake is completed.
				s.handshakeComplete = true
				aeadChanged = nil // prevent this case from ever being selected again
				s.paths[protocol.InitialPathID].sentPacketHandler.SetHandshakeComplete()
				close(s.handshakeChan)
				close(s.handshakeCompleteChan)
			} else {
				s.tryDecryptingQueuedPackets()
				s.handshakeChan <- handshakeEvent{encLevel: l}
			}
		}

		now := time.Now()
		if timerPth != nil {
			if timeout := timerPth.sentPacketHandler.GetAlarmTimeout(); !timeout.IsZero() && timeout.Before(now) {
				// This could cause packets to be retransmitted, so check it before trying
				// to send packets.
				timerPth.sentPacketHandler.OnAlarm()
			}
			timerPth = nil
		}

		if !s.pathManagerLaunched && s.handshakeComplete {
			// XXX (QDC): for benchmark tests
			if s.pathManager != nil {
				s.pathManager.handshakeCompleted <- struct{}{}
				s.pathManagerLaunched = true
			}
		}

		if s.config.KeepAlive && s.handshakeComplete && time.Since(s.lastNetworkActivityTime) >= s.peerParams.IdleTimeout/2 {
			// send the PING frame since there is no activity in the session
			s.pathsLock.RLock()
			// XXX (QDC): send PING over all paths, but is it really needed/useful?
			for _, tmpPth := range s.paths {
				if !tmpPth.active.Get() {
					continue
				}
				s.packer.QueueControlFrame(&wire.PingFrame{}, tmpPth)
			}
			s.pathsLock.RUnlock()
			s.keepAlivePingSent = true
		}

		if false {
			//  If we are application-limited, we try to opportunistically send reinjections of the in-flight packets on shorter paths
			// TODO disabled for now, this seems to cause issues?
			if !s.streamFramer.HasFramesToSend() {
				// For each path, we take the packets currently in flight and try to reinject them if they have not already been reinjected
				for _, pathToReinject := range s.paths {
					for _, pkt := range pathToReinject.sentPacketHandler.GetPacketsInFlight() {
						for _, path := range s.paths {
							// TODO: we could assume that the One-Way Delay is 1/2*RTT and not duplicate a packet if it has
							// already been sent for 1/2*RTT
							if path != pathToReinject && path.rttStats.SmoothedRTT() < pathToReinject.rttStats.SmoothedRTT() {
								pkt.Duplicated = true
								path.sentPacketHandler.DuplicatePacket(pkt)
							}
						}
					}
				}
			}
		}

		if err := s.sendPacket(); err != nil {
			s.closeLocal(err)
		}

		if !s.receivedTooManyUndecrytablePacketsTime.IsZero() && s.receivedTooManyUndecrytablePacketsTime.Add(protocol.PublicResetTimeout).Before(now) && len(s.undecryptablePackets) != 0 {
			s.closeLocal(qerr.Error(qerr.DecryptionFailure, "too many undecryptable packets received"))
		}
		if !s.handshakeComplete && now.Sub(s.sessionCreationTime) >= s.config.HandshakeTimeout {
			s.closeLocal(qerr.Error(qerr.HandshakeTimeout, "Crypto handshake did not complete in time."))
		}
		if s.handshakeComplete && now.Sub(s.lastNetworkActivityTime) >= s.config.IdleTimeout {
			s.closeLocal(qerr.Error(qerr.NetworkIdleTimeout, "No recent network activity."))
		}

		// Check if we should send a PATHS frame (currently hardcoded at 200 ms) only when at least one stream is open (not counting streams 1 and 3 never closed...)
		if s.pathManager != nil && s.handshakeComplete && s.version >= protocol.VersionMP && now.Sub(s.lastPathsFrameSent) >= 200*time.Millisecond && len(s.streamsMap.openStreams) > 2 {
			// XXX Ugly but needed...
			s.pathManager.pconnMgr.PconnsLock().RLock()
			s.pathsLock.RLock()
			s.SchedulePathsFrame()
			s.pathsLock.RUnlock()
			s.pathManager.pconnMgr.PconnsLock().RUnlock()
		}

		if err := s.streamsMap.DeleteClosedStreams(); err != nil {
			s.closeLocal(err)
		}
	}

	// only send the error the handshakeChan when the handshake is not completed yet
	// otherwise this chan will already be closed
	if !s.handshakeComplete {
		s.handshakeCompleteChan <- closeErr.err
		s.handshakeChan <- handshakeEvent{err: closeErr.err}
	}
	s.handleCloseError(closeErr)
	return closeErr.err
}

func (s *session) Context() context.Context {
	return s.ctx
}

func (s *session) maybeResetTimer() {
	var deadline time.Time
	if s.config.KeepAlive && s.handshakeComplete && !s.keepAlivePingSent {
		deadline = s.lastNetworkActivityTime.Add(s.peerParams.IdleTimeout / 2)
	} else {
		deadline = s.lastNetworkActivityTime.Add(s.config.IdleTimeout)
	}

	if !s.handshakeComplete {
		handshakeDeadline := s.sessionCreationTime.Add(s.config.HandshakeTimeout)
		deadline = utils.MinTime(deadline, handshakeDeadline)
	}
	if !s.receivedTooManyUndecrytablePacketsTime.IsZero() {
		deadline = utils.MinTime(deadline, s.receivedTooManyUndecrytablePacketsTime.Add(protocol.PublicResetTimeout))
	}

	s.timer.Reset(deadline)
}

func (s *session) handlePacketImpl(p *receivedPacket) error {
	if s.perspective == protocol.PerspectiveClient {
		diversificationNonce := p.header.DiversificationNonce
		if len(diversificationNonce) > 0 {
			s.cryptoSetup.SetDiversificationNonce(diversificationNonce)
		}
	}

	if p.rcvTime.IsZero() {
		// To simplify testing
		p.rcvTime = time.Now()
	}

	s.lastNetworkActivityTime = p.rcvTime
	/// XXX (QDC): see if this should be brought at path level too
	s.keepAlivePingSent = false

	var pth *path
	var ok bool
	var err error

	if p.recovered {
		//pth = s.paths[protocol.InitialPathID]
		pth, ok = s.paths[p.header.PathID]
		if !ok {
			panic("recovered packet was sent on unknown path!")
		}
	} else {

		if p.header.PathID > s.maxPathID {
			// XXX: Drop the packet without error?
			return nil
		}

		pth, ok = s.paths[p.header.PathID]
		if !ok {
			// It's a new path initiated from remote host
			pth, err = s.pathManager.createPathFromRemote(p)
			if err != nil {
				return err
			}
		}

	}
	var oldRemAddr net.Addr
	if pth.conn != nil {
		oldRemAddr = pth.conn.RemoteAddr()
	}

	packet, err := pth.handlePacketImpl(p)
	if err != nil {
		return err
	}

	err = s.handleFrames(packet.frames, packet.encryptionLevel, pth)

	pth.rttStats.Windows = append(pth.rttStats.Windows, map[uint64]protocol.ByteCount{uint64(time.Now().UnixNano()): pth.sentPacketHandler.GetSendAlgorithm().GetCongestionWindow()})
	// Now we potentially processed the PATHS frame with remote address ID, update remote address of all paths using the same remote address
	// ID, to cope with, e.g., NAT rebinding detected on one of the paths.
	if s.perspective == protocol.PerspectiveServer && oldRemAddr != p.remoteAddr {
		s.pathsLock.Lock()
		for _, tmpPth := range s.paths {
			if tmpPth == pth || !tmpPth.active.Get() {
				continue
			}
			if tmpPth.remAddrID == pth.remAddrID {
				tmpPth.conn.SetCurrentRemoteAddr(p.remoteAddr)
			}
		}
		s.pathsLock.Unlock()
	}

	// tell the redundancy controller about the path's properties
	if pth.pathID != protocol.InitialPathID && pth.rttStats.SmoothedRTT().Nanoseconds() > 0 {
		oSender := s.pathManager.oliaSenders[pth.pathID]
		s.redundancyController.InsertMeasurement(
			pth.pathID,
			oSender,
			*pth.rttStats,
			pth.sentPacketHandler,
		)
	}

	return err
}

func (s *session) handleFrames(fs []wire.Frame, encLevel protocol.EncryptionLevel, p *path) error {
	for _, ff := range fs {
		var err error
		wire.LogFrame(ff, false)
		switch frame := ff.(type) {
		case *wire.StreamFrame:
			err = s.handleStreamFrame(frame)
		case *wire.AckFrame:
			err = s.handleAckFrame(frame, encLevel)
		case *wire.ConnectionCloseFrame:
			s.closeRemote(qerr.Error(frame.ErrorCode, frame.ReasonPhrase))
		case *wire.GoawayFrame:
			err = errors.New("unimplemented: handling GOAWAY frames")
		case *wire.StopWaitingFrame:
			// LeastUnacked is guaranteed to have LeastUnacked > 0
			// therefore this will never underflow
			p.receivedPacketHandler.SetLowerLimit(frame.LeastUnacked - 1)
		case *wire.RstStreamFrame:
			err = s.handleRstStreamFrame(frame)
		case *wire.FECFrame:
			if _, ok := s.receiverFECScheme.(fec.ConvolutionalFECScheme); ok {
				s.fecFrameworkReceiverConvolutional.handleFECFrame(frame)
			} else {
				s.fecFrameworkReceiver.handleFECFrame(frame)
			}
		case *wire.RecoveredFrame:
			s.handleRecoveredFrame(frame, p.pathID, encLevel)
		case *wire.MaxDataFrame:
			s.handleMaxDataFrame(frame)
		case *wire.MaxStreamDataFrame:
			err = s.handleMaxStreamDataFrame(frame)
		case *wire.BlockedFrame:
			s.SetPeerBlocked(true)
		case *wire.StreamBlockedFrame:
			// TODO per-stream MAX_DATA management
			s.SetPeerBlocked(true)
		case *wire.PingFrame:
		case *wire.AddAddressFrame:
			if s.pathManager != nil {
				s.pathManager.handleAddAddressFrame(frame)
				s.pathManager.pconnMgr.PconnsLock().RLock()
				s.pathsLock.RLock()
				s.SchedulePathsFrame()
				s.pathsLock.RUnlock()
				s.pathManager.pconnMgr.PconnsLock().RUnlock()
			}
		case *wire.RemoveAddressFrame:
			if s.pathManager != nil {
				s.pathManager.handleRemoveAddressFrame(frame)
				s.pathManager.pconnMgr.PconnsLock().RLock()
				s.pathsLock.RLock()
				s.SchedulePathsFrame()
				s.pathsLock.RUnlock()
				s.pathManager.pconnMgr.PconnsLock().RUnlock()
			}
		case *wire.PathsFrame:
			s.pathsLock.RLock()
			for k, pathInfo := range frame.PathInfos {
				s.remoteRTTs[k] = pathInfo.RTT
				// Completely trust the remote, if paths exists
				// FIXME what to do when a new path is indicated?
				pth, ok := s.paths[k]
				if ok {
					if pth.remAddrID != pathInfo.AddrID {
						s.paths[k].remAddrID = pathInfo.AddrID
					}
					bk, ok := s.PathManager().remoteBackups[pathInfo.AddrID]
					if ok {
						bk = bk || pth.backup.Get()
						pth.backup.Set(bk)
					}
				} else {
					// The path might not be created yet, keep the remote Addr ID
					s.PathManager().remoteAddrIDOfComingPaths[k] = pathInfo.AddrID
				}
			}
			s.pathsLock.RUnlock()
		default:
			return errors.New("Session BUG: unexpected frame type")
		}

		if err != nil {
			switch err {
			case ackhandler.ErrDuplicateOrOutOfOrderAck:
				// Can happen e.g. when packets thought missing arrive late
			case errRstStreamOnInvalidStream:
				// Can happen when RST_STREAMs arrive early or late (?)
				utils.Errorf("Ignoring error in session: %s", err.Error())
			case errWindowUpdateOnClosedStream:
				// Can happen when we already sent the last StreamFrame with the FinBit, but the client already sent a WindowUpdate for this Stream
			default:
				return err
			}
		}
	}
	return nil
}

// handlePacket is called by the server with a new packet
func (s *session) handlePacket(p *receivedPacket) {
	// Discard packets once the amount of queued packets is larger than
	// the channel size, protocol.MaxSessionUnprocessedPackets
	// XXX (QDC): Multipath still rely on one buffer for the connection;
	// in the future, it might make more sense to first buffer in the
	// path and then give it to the connection...
	select {
	case s.receivedPackets <- p:
	default:
	}
}

func (s *session) handleStreamFrame(frame *wire.StreamFrame) error {
	if frame.StreamID == s.version.CryptoStreamID() {
		if frame.Unreliable {
			return qerr.Error(qerr.UnreliableStreamFrameOnStream1, fmt.Sprintf("Unreliable stream frame received for Stream ID 1"))
		}
		return s.cryptoStream.AddStreamFrame(frame)
	}
	str, err := s.streamsMap.GetOrOpenStream(frame.StreamID)
	if err != nil {
		return err
	}
	if str == nil {
		// Stream is closed and already garbage collected
		// ignore this StreamFrame
		return nil
	}
	if frame.FinBit {
		// Receiving end of stream, print stats about it
		// Print client statistics about its paths
		s.pathsLock.RLock()
		utils.Infof("Info for stream %x of %x", frame.StreamID, s.connectionID)
		for pathID, pth := range s.paths {
			sntPkts, sntRetrans, sntLost := pth.sentPacketHandler.GetStatistics()
			rcvPkts, recoveredPkts := pth.receivedPacketHandler.GetStatistics()
			utils.Infof("Path %x: sent %d retrans %d lost %d; rcv %d, recovered %d", pathID, sntPkts, sntRetrans, sntLost, rcvPkts, recoveredPkts)
		}
		s.pathsLock.RUnlock()
	}
	if frame.Unreliable {
		str.SetUnreliable(true)
	}
	return str.AddStreamFrame(frame)
}

func (s *session) handleMaxDataFrame(frame *wire.MaxDataFrame) {
	s.connFlowController.UpdateSendWindow(frame.ByteOffset)
}

func (s *session) handleMaxStreamDataFrame(frame *wire.MaxStreamDataFrame) error {
	str, err := s.streamsMap.GetOrOpenStream(frame.StreamID)
	if err != nil {
		return err
	}
	if str == nil {
		return errWindowUpdateOnClosedStream
	}
	str.UpdateSendWindow(frame.ByteOffset)
	return nil
}

func (s *session) handleRstStreamFrame(frame *wire.RstStreamFrame) error {
	str, err := s.streamsMap.GetOrOpenStream(frame.StreamID)
	if err != nil {
		return err
	}
	if str == nil {
		return errRstStreamOnInvalidStream
	}
	return str.RegisterRemoteError(fmt.Errorf("RST_STREAM received with code %d", frame.ErrorCode), frame.ByteOffset)
}

func (s *session) handleAckFrame(frame *wire.AckFrame, encLevel protocol.EncryptionLevel) error {
	s.scheduler.receivedACKForRL(s.paths, frame)
	pth := s.paths[frame.PathID]
	err := pth.sentPacketHandler.ReceivedAck(frame, pth.lastRcvdPacketNumber, encLevel, pth.lastNetworkActivityTime)
	if err == nil && pth.rttStats.SmoothedRTT() > s.rttStats.SmoothedRTT() {
		// Update the session RTT, which comes to take the max RTT on all paths
		s.rttStats.UpdateSessionRTT(pth.rttStats.SmoothedRTT())
	}
	return err
}

func (s *session) handleRecoveredFrame(frame *wire.RecoveredFrame, pid protocol.PathID, encLevel protocol.EncryptionLevel) error {
	pth := s.paths[pid]
	log.Printf("HANDLE RECOVERED FRAME:")
	for _, r := range frame.RecoveredRanges {
		log.Printf("FROM %+v TO %+v", r.First, r.Last)
	}
	err := pth.sentPacketHandler.ReceivedRecoveredFrame(frame, encLevel)
	return err
}

// SchedulePathsFrame MUST hold pconnsLock and pathsLock!
func (s *session) SchedulePathsFrame() {
	s.lastPathsFrameSent = time.Now()
	s.streamFramer.AddPathsFrameForTransmission(s)
}

func (s *session) closePaths() {
	// XXX (QDC): still for tests
	if s.pathManager != nil {
		s.pathManager.closePaths()
		if s.pathManager.pconnMgr == nil {
			// XXX For tests
			s.paths[0].conn.Close()
		}
		// wait for the run loops of path to finish
		s.pathManager.wg.Wait()
	} else {
		s.pathsLock.RLock()
		for _, pth := range s.paths {
			select {
			case pth.closeChan <- nil:
			default:
				// Don't block
			}
		}
		s.pathsLock.RUnlock()
		// no waiting time...
	}

}

func (s *session) closeLocal(e error) {
	s.closeOnce.Do(func() {
		s.closeChan <- closeError{err: e, remote: false}
	})
}

func (s *session) closeRemote(e error) {
	s.closeOnce.Do(func() {
		s.closeChan <- closeError{err: e, remote: true}
	})
}

// Close the connection. If err is nil it will be set to qerr.PeerGoingAway.
// It waits until the run loop has stopped before returning
func (s *session) Close(e error) error {
	s.closeLocal(e)
	<-s.ctx.Done()
	return nil
}

func (s *session) handleCloseError(closeErr closeError) error {
	if closeErr.err == nil {
		closeErr.err = qerr.PeerGoingAway
	}

	var quicErr *qerr.QuicError
	var ok bool
	if quicErr, ok = closeErr.err.(*qerr.QuicError); !ok {
		quicErr = qerr.ToQuicError(closeErr.err)
	}
	// Don't log 'normal' reasons
	if quicErr.ErrorCode == qerr.PeerGoingAway || quicErr.ErrorCode == qerr.NetworkIdleTimeout {
		utils.Infof("Closing connection %x", s.connectionID)
	} else {
		utils.Errorf("Closing session with error: %s", closeErr.err.Error())
	}

	s.cryptoStream.Cancel(quicErr)
	s.streamsMap.CloseWithError(quicErr)

	if closeErr.err == errCloseSessionForNewVersion {
		return nil
	}

	s.closePaths()

	// If this is a remote close we're done here
	if closeErr.remote {
		return nil
	}

	if quicErr.ErrorCode == qerr.DecryptionFailure ||
		quicErr == handshake.ErrHOLExperiment ||
		quicErr == handshake.ErrNSTPExperiment {
		// XXX seems reasonable to send public reset on path ID 0, but this can change
		return s.sendPublicReset(s.paths[0].lastRcvdPacketNumber)
	}
	return s.sendConnectionClose(quicErr)
}

func (s *session) processTransportParameters(params *handshake.TransportParameters) {
	// TODO: handle fecSchemeID
	s.peerParams = params
	s.streamsMap.UpdateMaxStreamLimit(params.MaxStreams)
	if params.OmitConnectionID {
		s.packer.SetOmitConnectionID()
	}
	s.connFlowController.UpdateSendWindow(params.ConnectionFlowControlWindow)
	s.streamsMap.Range(func(str streamI) {
		str.UpdateSendWindow(params.StreamFlowControlWindow)
	})
	s.maxPathID = protocol.PathID(s.config.MaxPathID)
	if params.MaxPathID < s.maxPathID {
		s.maxPathID = params.MaxPathID
	}
	log.Printf("PROCESS TRANSPORT PARAMS %+v", params.FECScheme)
	s.senderFECScheme, _ = GetFECSchemeFromID(params.FECScheme)
	s.fecFrameworkSender.fecScheme = s.senderFECScheme
}

func (s *session) sendPacket() error {
	// XXX This is ugly, but needed...
	if s.pathManager != nil {
		s.pathManager.pconnMgr.PconnsLock().RLock()
	}
	s.pathsLock.RLock()
	err := s.scheduler.sendPacket(s)
	s.pathsLock.RUnlock()
	if s.pathManager != nil {
		s.pathManager.pconnMgr.PconnsLock().RUnlock()
	}
	return err
}

func (s *session) sendPackedPacket(packet *packedPacket, pth *path) error {
	defer putPacketBuffer(packet.raw)
	err := pth.sentPacketHandler.SentPacket(&ackhandler.Packet{
		PacketNumber:    packet.header.PacketNumber,
		Frames:          packet.frames,
		Length:          protocol.ByteCount(len(packet.raw)),
		EncryptionLevel: packet.encryptionLevel,
	})
	if err != nil {
		return err
	}
	pth.sentPacket <- struct{}{}

	s.redundancyController.OnPacketSent(packet.header.PacketNumber, packet.containsOnlyFECFrames)
	s.logPacket(packet, pth.pathID)

	if pth.conn == nil {
		// Don't panic, but don't raise error either
		return nil
	}

	logger.ExpLogInsertPacket(len(packet.raw), pth.conn.RemoteAddr(), packet.containsOnlyFECFrames)
	logger.ExpLogInsertCwnd(pth.conn.RemoteAddr(), pth.GetCongestionWindow(), pth.GetCongestionWindowFree())

	return pth.conn.Write(packet.raw)
}

func (s *session) sendConnectionClose(quicErr *qerr.QuicError) error {
	s.paths[0].SetLeastUnacked(s.paths[0].sentPacketHandler.GetLeastUnacked())
	packet, err := s.packer.PackConnectionClose(&wire.ConnectionCloseFrame{
		ErrorCode:    quicErr.ErrorCode,
		ReasonPhrase: quicErr.ErrorMessage,
	}, s.paths[0])
	if err != nil {
		return err
	}
	s.logPacket(packet, protocol.InitialPathID)
	// XXX (QDC): seems reasonable to send on pathID 0, but this can change
	return s.paths[protocol.InitialPathID].conn.Write(packet.raw)
}

func (s *session) SendPing(pth *path) error {
	packet, err := s.packer.PackPing(&wire.PingFrame{}, pth)
	if err != nil {
		return err
	}
	if packet == nil {
		return errors.New("Session BUG: expected ping packet not to be nil")
	}
	return s.sendPackedPacket(packet, pth)
}

func (s *session) logPacket(packet *packedPacket, pathID protocol.PathID) {
	if !utils.Debug() {
		// We don't need to allocate the slices for calling the format functions
		return
	}
	utils.Debugf("-> Sending packet 0x%x (%d bytes) for connection %x on path %x, %s", packet.header.PacketNumber, len(packet.raw), s.connectionID, pathID, packet.encryptionLevel)
	for _, frame := range packet.frames {
		wire.LogFrame(frame, true)
	}
}

// GetOrOpenStream either returns an existing stream, a newly opened stream, or nil if a stream with the provided ID is already closed.
// Newly opened streams should only originate from the client. To open a stream from the server, OpenStream should be used.
func (s *session) GetOrOpenStream(id protocol.StreamID) (Stream, error) {
	str, err := s.streamsMap.GetOrOpenStream(id)
	if str != nil {
		return str, err
	}
	// make sure to return an actual nil value here, not an Stream with value nil
	return nil, err
}

// AcceptStream returns the next stream openend by the peer
func (s *session) AcceptStream() (Stream, error) {
	return s.streamsMap.AcceptStream()
}

// OpenStream opens a stream
func (s *session) OpenStream() (Stream, error) {
	return s.streamsMap.OpenStream()
}

func (s *session) OpenStreamSync() (Stream, error) {
	return s.streamsMap.OpenStreamSync()
}

func (s *session) WaitUntilHandshakeComplete() error {
	return <-s.handshakeCompleteChan
}

func (s *session) IsHandshakeComplete() bool {
	return s.handshakeComplete
}

func (s *session) queueResetStreamFrame(id protocol.StreamID, offset protocol.ByteCount) {
	s.packer.QueueControlFrame(&wire.RstStreamFrame{
		StreamID:   id,
		ByteOffset: offset,
	}, s.paths[protocol.InitialPathID])
	s.scheduleSending()
}

func (s *session) newStream(id protocol.StreamID) streamI {
	var initialSendWindow protocol.ByteCount
	if s.peerParams != nil {
		initialSendWindow = s.peerParams.StreamFlowControlWindow
	}
	flowController := flowcontrol.NewStreamFlowController(
		id,
		s.version.StreamContributesToConnectionFlowControl(id),
		s.connFlowController,
		protocol.ReceiveStreamFlowControlWindow,
		protocol.ByteCount(s.config.MaxReceiveStreamFlowControlWindow),
		initialSendWindow,
		s.rttStats,
		s.remoteRTTs,
	)
	return newStream(id, s.scheduleSending, s.queueResetStreamFrame, flowController, s.version)
}

func (s *session) sendPublicReset(rejectedPacketNumber protocol.PacketNumber) error {
	utils.Infof("Sending public reset for connection %x, packet number %d", s.connectionID, rejectedPacketNumber)
	// XXX: seems reasonable to send on the pathID 0, but this can change
	return s.paths[protocol.InitialPathID].conn.Write(wire.WritePublicReset(s.connectionID, rejectedPacketNumber, 0))
}

// scheduleSending signals that we have data for sending
func (s *session) scheduleSending() {
	select {
	case s.sendingScheduled <- struct{}{}:
	default:
	}
}

// scheduleFECSending signals that we have FEC data for sending
func (s *session) scheduleFECSending() {
	select {
	case s.fecScheduled <- struct{}{}:
	default:
	}
}

func (s *session) onHasFECData() {
	s.scheduleFECSending()
}

func (s *session) tryQueueingUndecryptablePacket(p *receivedPacket) {
	if s.handshakeComplete {
		utils.Debugf("Received undecryptable packet from %s after the handshake: %#v, %d bytes data", p.remoteAddr.String(), p.header, len(p.data))
		return
	}
	if len(s.undecryptablePackets)+1 > protocol.MaxUndecryptablePackets {
		// if this is the first time the undecryptablePackets runs full, start the timer to send a Public Reset
		if s.receivedTooManyUndecrytablePacketsTime.IsZero() {
			s.receivedTooManyUndecrytablePacketsTime = time.Now()
			s.maybeResetTimer()
		}
		utils.Infof("Dropping undecrytable packet 0x%x (undecryptable packet queue full)", p.header.PacketNumber)
		return
	}
	utils.Infof("Queueing packet 0x%x for later decryption", p.header.PacketNumber)
	s.undecryptablePackets = append(s.undecryptablePackets, p)
}

func (s *session) tryDecryptingQueuedPackets() {
	for _, p := range s.undecryptablePackets {
		s.handlePacket(p)
	}
	s.undecryptablePackets = s.undecryptablePackets[:0]
}

func (s *session) getWindowUpdates(force bool) []wire.Frame {
	var res []wire.Frame
	s.streamsMap.Range(func(str streamI) {
		if offset := str.GetWindowUpdate(force); offset != 0 {
			res = append(res, &wire.MaxStreamDataFrame{
				StreamID:   str.StreamID(),
				ByteOffset: offset,
			})
		}
	})
	if offset := s.connFlowController.GetWindowUpdate(force); offset != 0 {
		res = append(res, &wire.MaxDataFrame{
			ByteOffset: offset,
		})
	}
	return res
}

func (s *session) LocalAddr() net.Addr {
	// XXX (QDC): do it like with MPTCP (master initial path), what if it is closed?
	return s.paths[0].conn.LocalAddr()
}

// RemoteAddr returns the net.Addr of the client
func (s *session) RemoteAddr() net.Addr {
	// XXX (QDC): do it like with MPTCP (master initial path), what if it is closed?
	return s.paths[0].conn.RemoteAddr()
}

func (s *session) GetVersion() protocol.VersionNumber {
	return s.version
}

func (s *session) GetCryptoSetup() handshake.CryptoSetup {
	return s.cryptoSetup
}

func (s *session) GetConfig() *Config {
	return s.config
}

func (s *session) GetUnpacker() unpacker {
	return s.unpacker
}

func (s *session) GetPerspective() protocol.Perspective {
	return s.perspective
}

func (s *session) PathTimersChan() chan *path {
	return s.pathTimers
}

func (s *session) Paths() map[protocol.PathID]*path {
	return s.paths
}

func (s *session) PathsLock() *sync.RWMutex {
	return &s.pathsLock
}

func (s *session) GetMaxPathID() protocol.PathID {
	return s.maxPathID
}

func (s *session) GetStreamFramer() *streamFramer {
	return s.streamFramer
}

func (s *session) GetPacker() *packetPacker {
	return s.packer
}

func (s *session) GetConnectionID() protocol.ConnectionID {
	return s.connectionID
}

func (s *session) SetPeerBlocked(peerBlocked bool) {
	s.peerBlocked = peerBlocked
}

func (s *session) GetPeerBlocked() bool {
	return s.peerBlocked
}

func (s *session) PathManager() *pathManager {
	return s.pathManager
}

func (s *session) GetFECFramer() *FECFramer {
	return s.fecFramer
}

func (s *session) SetFECScheme(f fec.FECScheme) {
	s.receiverFECScheme = f
	if f2, ok := f.(fec.BlockFECScheme); ok {
		if s.fecFrameworkReceiver == nil {
			s.fecFrameworkReceiver = NewFECFrameworkReceiver(s, f2)
		}
		s.fecFrameworkReceiver.fecScheme = f2
	} else {
		if s.fecFrameworkReceiverConvolutional == nil {
			s.fecFrameworkReceiverConvolutional = NewFECFrameworkReceiverConvolutional(s, f.(fec.ConvolutionalFECScheme))
		}
		s.fecFrameworkReceiverConvolutional.fecScheme = f.(fec.ConvolutionalFECScheme)
	}
}

func (s *session) GetFECScheme() fec.FECScheme {
	return s.receiverFECScheme
}

func (s *session) GetFECFrameworkReceiver() *FECFrameworkReceiver {
	return s.fecFrameworkReceiver
}

func (s *session) GetFECFrameworkConvolutionalReceiver() *FECFrameworkReceiverConvolutional {
	return s.fecFrameworkReceiverConvolutional
}

func (s *session) GetFECFrameworkSender() *FECFrameworkSender {
	return s.fecFrameworkSender
}

func (s *session) SetRedundancyController(c fec.RedundancyController) {
	s.redundancyController = c
	s.fecScheduler.SetRedundancyController(c)
	s.fecFrameworkSender.redundancyController = c
	s.scheduler.redundancyController = c
}

func (s *session) GetRedundancyController() fec.RedundancyController {
	return s.redundancyController
}

func GetFECSchemeFromID(id protocol.FECSchemeID) (fec.FECScheme, error) {
	switch id {
	case protocol.XORFECScheme:
		return &fec.XORFECScheme{}, nil
	case protocol.ReedSolomonFECScheme:
		return fec.NewReedSolomonFECScheme()
	case protocol.RLCFECScheme:
		return fec.NewRandomLinearFECScheme(), nil
	default:
		return nil, errors.New(fmt.Sprintf("There is no FEC Scheme "))
	}
}

func (s *session) GetOpenStreamNo() uint32 {
	return uint32(len(s.streamsMap.streams))
}

func (s *session) RemoveStream(stream protocol.StreamID) {
	s.streamsMap.CloseStream(stream)
}

func (s *session) GetLargestRcvdPacketNumber() protocol.PacketNumber {
	return s.largestRcvdPacketNumber
}

func (s *session) MaybeSetLargestRcvdPacketNumber(p protocol.PacketNumber) {
	s.largestRcvdPacketNumber = utils.MaxPacketNumber(s.largestRcvdPacketNumber, p)
}

func (s *session) GetStreamMap() (*streamsMap, error) {
	return s.streamsMap, nil
}

func (s *session) GetPaths() map[protocol.PathID]*path {
	return  s.paths
}

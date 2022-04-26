package quic

import (
	"errors"
	"net"
	"sync"
	"time"

	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
	// reuse "github.com/jbenet/go-reuseport"
)

type pconnManagerI interface {
	AddedAddrIDChan() chan protocol.AddressID
	GetPreferredPort() int
	RemovedAddrIDChan() chan protocol.AddressID
	LocalAddrs() map[protocol.AddressID]*net.UDPAddr
	Pconns() map[protocol.AddressID]net.PacketConn
	PconnsLock() *sync.RWMutex
	GetAddrIDOf(addr net.Addr) (protocol.AddressID, bool)
	GetInterfaceName(addr net.UDPAddr) (string, bool)
}

type receivedRawPacket struct {
	rcvPconn   net.PacketConn
	remoteAddr net.Addr
	data       []byte
	rcvTime    time.Time
}

type pconnManager struct {
	pconnsLock sync.RWMutex
	pconns     map[protocol.AddressID]net.PacketConn

	netWatch NetWatcherI

	localAddrs   map[protocol.AddressID]*net.UDPAddr
	localAddrIDS map[string]protocol.AddressID // Quick way to access address ID

	nxtAddrID protocol.AddressID

	perspective protocol.Perspective

	// Useful at server side, to avoid announcing ephemeral ports
	preferredPort int

	// For darwin, take the configuration to find the Notify ID
	config *Config

	rcvRawPackets chan *receivedRawPacket

	addedAddrID   chan protocol.AddressID
	removedAddrID chan protocol.AddressID

	closeConns chan struct{}
	closed     chan struct{}
	errorConn  chan error
}

var _ pconnManagerI = &pconnManager{}

// Setup the pconn_manager and the pconnAny connection
func (pcm *pconnManager) setup(pconnArg net.PacketConn, listenAddr net.Addr, newNetWatcher func(i *pconnManager) NetWatcherI) error {
	pcm.pconns = make(map[protocol.AddressID]net.PacketConn)
	pcm.localAddrs = make(map[protocol.AddressID]*net.UDPAddr)
	pcm.localAddrIDS = make(map[string]protocol.AddressID)
	pcm.rcvRawPackets = make(chan *receivedRawPacket)
	pcm.addedAddrID = make(chan protocol.AddressID, 5)
	pcm.removedAddrID = make(chan protocol.AddressID, 5)
	pcm.closeConns = make(chan struct{}, 1)
	pcm.closed = make(chan struct{}, 1)
	pcm.errorConn = make(chan error, 1) // Made non-blocking for tests
	pcm.nxtAddrID = protocol.AddressID(1)

	if pconnArg == nil {
		addr := &net.UDPAddr{IP: net.IPv4zero, Port: 0}
		pconn, err := net.ListenUDP("udp", addr)
		// pconn, err := reuse.ListenPacket("udp", listenAddrStr)
		if err != nil {
			utils.Errorf("pconn_manager: %v", err)
			// Format for expected consistency
			operr := &net.OpError{Op: "listen", Net: "udp", Source: listenAddr, Addr: listenAddr, Err: err}
			return operr
		}
		pcm.pconns[0] = pconn
		pconnAddr := pconn.LocalAddr().(*net.UDPAddr)
		pcm.localAddrs[0] = pconnAddr
		pcm.localAddrIDS[pconnAddr.String()] = 0
	} else {
		pcm.pconns[0] = pconnArg
		addr := pconnArg.LocalAddr().(*net.UDPAddr)
		pcm.preferredPort = addr.Port
		pcm.localAddrs[0] = addr
		pcm.localAddrIDS[addr.String()] = 0
	}

	if utils.Debug() {
		utils.Debugf("Created pconn_manager, first path on %s", pcm.pconns[0].LocalAddr().String())
	}

	pcm.netWatch = newNetWatcher(pcm)

	//pcm.netWatch = &netWatcher{
	//	pconnMgr: pcm,
	//}

	// First start to listen to the sockets
	go pcm.listen(pcm.pconns[0])
	pcm.addedAddrID <- 0

	// Run the watcher
	pcm.netWatch.setup()

	return nil
}

func (pcm *pconnManager) listen(pconn net.PacketConn) {
	var err error

listenLoop:
	for {
		var n int
		var addr net.Addr
		data := getPacketBuffer()
		data = data[:protocol.MaxReceivePacketSize]
		// The packet size should not exceed protocol.MaxReceivePacketSize bytes
		// If it does, we only read a truncate packet, which will then end up undecryptable
		n, addr, err = pconn.ReadFrom(data)
		if err != nil {
			// XXX (QDC): this is a little hacky, but it reflects that we could have network handover if there is another address ready to use
			if len(pcm.pconns) <= 1 {
				pcm.errorConn <- err
			}
			pcm.netWatch.lostPconn()
			break listenLoop
		}
		data = data[:n]

		rcvRawPacket := &receivedRawPacket{
			rcvPconn:   pconn,
			remoteAddr: addr,
			data:       data,
			rcvTime:    time.Now(),
		}

		pcm.rcvRawPackets <- rcvRawPacket
	}
}

func (pcm *pconnManager) createPconn(ip net.IP) (*net.UDPAddr, error) {
	pconn, err := net.ListenUDP("udp", &net.UDPAddr{IP: ip, Port: 0})
	if err != nil {
		return nil, err
	}
	locAddr, err := net.ResolveUDPAddr("udp", pconn.LocalAddr().String())
	if err != nil {
		return nil, err
	}
	// This is needed for a complete syncronization...
	select {
	case s, ok := <-pcm.closeConns:
		if ok {
			// Replay it, and emit an error
			pcm.closeConns <- s
		}
		return nil, errors.New("closing pconns")
	default:
		// Don't block
	}
	pcm.pconnsLock.Lock()
	addrID := pcm.nxtAddrID
	pcm.pconns[addrID] = pconn
	pcm.localAddrs[addrID] = locAddr
	pcm.localAddrIDS[locAddr.String()] = addrID
	pcm.pconnsLock.Unlock()
	if utils.Debug() {
		utils.Debugf("Created pconn on %s", pconn.LocalAddr().String())
	}
	// Start to listen on this new socket
	go pcm.listen(pconn)
	pcm.addedAddrID <- addrID
	pcm.nxtAddrID++
	return locAddr, nil
}

func (pcm *pconnManager) closePconn(addr net.Addr) error {
	pcm.pconnsLock.Lock()
	addrID, ok := pcm.localAddrIDS[addr.String()]
	if !ok {
		return errors.New("address with no matching address ID")
	}
	pconn, ok := pcm.pconns[addrID]
	if !ok {
		return errors.New("pconn with no matching address ID")
	}
	utils.Infof("Closing %v with addrID %d", addr, addrID)
	pconn.Close()
	delete(pcm.pconns, addrID)
	delete(pcm.localAddrs, addrID)
	delete(pcm.localAddrIDS, addr.String())
	pcm.pconnsLock.Unlock()

	pcm.removedAddrID <- addrID
	return nil
}

func (pcm *pconnManager) closePconns() {
	for _, pconn := range pcm.pconns {
		pconn.Close()
	}
	close(pcm.closed)
}

func (pcm *pconnManager) AddedAddrIDChan() chan protocol.AddressID {
	return pcm.addedAddrID
}

func (pcm *pconnManager) RemovedAddrIDChan() chan protocol.AddressID {
	return pcm.removedAddrID
}

func (pcm *pconnManager) PconnsLock() *sync.RWMutex {
	return &pcm.pconnsLock
}

func (pcm *pconnManager) LocalAddrs() map[protocol.AddressID]*net.UDPAddr {
	return pcm.localAddrs
}

func (pcm *pconnManager) Pconns() map[protocol.AddressID]net.PacketConn {
	return pcm.pconns
}

// GetAddrIDOf MUST hold pconnsLock
func (pcm *pconnManager) GetAddrIDOf(addr net.Addr) (protocol.AddressID, bool) {
	addrID, ok := pcm.localAddrIDS[addr.String()]
	return addrID, ok
}

func (pcm *pconnManager) GetPreferredPort() int {
	return pcm.preferredPort
}

func (pcm *pconnManager) GetInterfaceName(addr net.UDPAddr) (string, bool) {
	return pcm.netWatch.getInterfaceName(addr)
}

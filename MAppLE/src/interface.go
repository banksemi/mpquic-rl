package quic

import (
	"context"
	"io"
	"net"
	"time"

	"github.com/lucas-clemente/quic-go/fec"
	"github.com/lucas-clemente/quic-go/internal/handshake"
	"github.com/lucas-clemente/quic-go/internal/protocol"
)

// The StreamID is the ID of a QUIC stream.
type StreamID = protocol.StreamID

// A VersionNumber is a QUIC version number.
type VersionNumber = protocol.VersionNumber

// A Cookie can be used to verify the ownership of the client address.
type Cookie = handshake.Cookie

// The number of bytes in QUIC
type ByteCount = protocol.ByteCount

// The MultipathServiceType expected by using multiple paths
type MultipathServiceType int

type FECSchemeID = protocol.FECSchemeID

const (
	XORFECScheme         FECSchemeID = protocol.XORFECScheme
	ReedSolomonFECScheme FECSchemeID = protocol.ReedSolomonFECScheme
	RLCFECScheme         FECSchemeID = protocol.RLCFECScheme
)

const (
	// Aggregate bandwidth with multiple paths
	Aggregate MultipathServiceType = iota
	// Handover support when changing networks
	Handover
)

// Stream is the interface implemented by QUIC streams
type Stream interface {
	// Read reads data from the stream.
	// Read can be made to time out and return a net.Error with Timeout() == true
	// after a fixed time limit; see SetDeadline and SetReadDeadline.
	io.Reader
	// Write writes data to the stream.
	// Write can be made to time out and return a net.Error with Timeout() == true
	// after a fixed time limit; see SetDeadline and SetWriteDeadline.
	io.Writer
	io.Closer
	StreamID() StreamID
	// Reset closes the stream with an error.
	Reset(error)
	// The context is canceled as soon as the write-side of the stream is closed.
	// This happens when Close() is called, or when the stream is reset (either locally or remotely).
	// Warning: This API should not be considered stable and might change soon.
	Context() context.Context
	// SetReadDeadline sets the deadline for future Read calls and
	// any currently-blocked Read call.
	// A zero value for t means Read will not time out.
	SetReadDeadline(t time.Time) error
	// SetWriteDeadline sets the deadline for future Write calls
	// and any currently-blocked Write call.
	// Even if write times out, it may return n > 0, indicating that
	// some of the data was successfully written.
	// A zero value for t means Write will not time out.
	SetWriteDeadline(t time.Time) error
	// SetDeadline sets the read and write deadlines associated
	// with the connection. It is equivalent to calling both
	// SetReadDeadline and SetWriteDeadline.
	SetDeadline(t time.Time) error
	// GetBytesSent returns the number of bytes of the stream that were sent to the peer
	GetBytesSent() ByteCount
	// GetBytesRetrans returns the number of bytes of the stream that were retransmitted to the peer
	GetBytesRetrans() ByteCount

	// Method for partial reliability

	// Returns true if the stream is a Unreliable Stream, false otherwise
	IsUnreliable() bool
	// Sets this stream as a Unreliable stream if val is true.
	// TODO: remove this method and implement it directly when opening a new Stream
	SetUnreliable(val bool)
	// Sets the retransmission deadline for this stream if it is unreliable
	SetRetransmissionDeadline(val time.Duration)
	GetRetransmissionDeadLine() time.Duration
	// the reliability dealine is the amount of time a reader is ok to wait on an unreliable stream before skipping data if the next data are not present
	SetReliabilityDeadline(val time.Duration)
	GetReliabilityDeadline() time.Duration

	// sets the stream replay buffer size. A stream will deliver its data as soon as the replay buffer is full or the stream is closed
	SetReplayBufferSize(size uint64)
	// gets the stream replay buffer size. A stream will deliver its data as soon as the replay buffer is full or the stream is closed
	GetReplayBufferSize() uint64

	SetMessageMode(val bool)
	GetMessageMode() bool
}

// A Session is a QUIC connection between two peers.
type Session interface {
	// AcceptStream returns the next stream opened by the peer, blocking until one is available.
	// Since stream 1 is reserved for the crypto stream, the first stream is either 2 (for a client) or 3 (for a server).
	AcceptStream() (Stream, error)
	// OpenStream opens a new QUIC stream, returning a special error when the peer's concurrent stream limit is reached.
	// New streams always have the smallest possible stream ID.
	// TODO: Enable testing for the special error
	OpenStream() (Stream, error)
	// OpenStreamSync opens a new QUIC stream, blocking until the peer's concurrent stream limit allows a new stream to be opened.
	// It always picks the smallest possible stream ID.
	OpenStreamSync() (Stream, error)
	// LocalAddr returns the local address.
	LocalAddr() net.Addr
	// RemoteAddr returns the address of the peer.
	RemoteAddr() net.Addr
	// Close closes the connection. The error will be sent to the remote peer in a CONNECTION_CLOSE frame. An error value of nil is allowed and will cause a normal PeerGoingAway to be sent.
	Close(error) error
	// The context is cancelled when the session is closed.
	// Warning: This API should not be considered stable and might change soon.
	Context() context.Context

	// sets the FEC Scheme used by this session
	SetFECScheme(scheme fec.FECScheme)
	// gets the FEC Scheme used by this session
	GetFECScheme() fec.FECScheme

	SetRedundancyController(c fec.RedundancyController)

	GetRedundancyController() fec.RedundancyController

	GetOpenStreamNo() uint32

	RemoveStream(stream protocol.StreamID)

	GetStreamMap() (*streamsMap, error)

	GetPaths() map[protocol.PathID]*path
}

// A NonFWSession is a QUIC connection between two peers half-way through the handshake.
// The communication is encrypted, but not yet forward secure.
type NonFWSession interface {
	Session
	WaitUntilHandshakeComplete() error
}

// Config contains all configuration data needed for a QUIC server or client.
type Config struct {
	// The QUIC versions that can be negotiated.
	// If not set, it uses all versions available.
	// Warning: This API should not be considered stable and will change soon.
	Versions []VersionNumber
	// Ask the server to omit the connection ID sent in the Public Header.
	// This saves 8 bytes in the Public Header in every packet. However, if the IP address of the server changes, the connection cannot be migrated.
	// Currently only valid for the client.
	RequestConnectionIDOmission bool
	// HandshakeTimeout is the maximum duration that the cryptographic handshake may take.
	// If the timeout is exceeded, the connection is closed.
	// If this value is zero, the timeout is set to 10 seconds.
	HandshakeTimeout time.Duration
	// IdleTimeout is the maximum duration that may pass without any incoming network activity.
	// This value only applies after the handshake has completed.
	// If the timeout is exceeded, the connection is closed.
	// If this value is zero, the timeout is set to 30 seconds.
	IdleTimeout time.Duration
	// AcceptCookie determines if a Cookie is accepted.
	// It is called with cookie = nil if the client didn't send an Cookie.
	// If not set, it verifies that the address matches, and that the Cookie was issued within the last 24 hours.
	// This option is only valid for the server.
	AcceptCookie func(clientAddr net.Addr, cookie *Cookie) bool
	// MaxReceiveStreamFlowControlWindow is the maximum stream-level flow control window for receiving data.
	// If this value is zero, it will default to 1 MB for the server and 6 MB for the client.
	MaxReceiveStreamFlowControlWindow uint64
	// MaxReceiveConnectionFlowControlWindow is the connection-level flow control window for receiving data.
	// If this value is zero, it will default to 1.5 MB for the server and 15 MB for the client.
	MaxReceiveConnectionFlowControlWindow uint64
	// KeepAlive defines whether this peer will periodically send PING frames to keep the connection alive.
	KeepAlive bool
	// Should we cache handshake parameters? If no cache available, should we create one?
	CacheHandshake bool
	// What is the maximum path ID that can be used over the connection?
	MaxPathID uint8
	// Was is the service expected by the use of multiple paths?
	MultipathService MultipathServiceType
	// A Notification ID, useful for darwin platform to notify network change
	NotifyID string
	// The ID of the FEC Scheme that we use to decode the FEC Frames
	FECScheme protocol.FECSchemeID
	// A redundancy controller that controls the amount of redundancy sent at any time.
	RedundancyController fec.RedundancyController
	// If set to true, recovered frames will bew sent when source symbols are recovered
	DisableFECRecoveredFrames bool

	ProtectReliableStreamFrames bool

	UseFastRetransmit bool

	OnlySendFECWhenApplicationLimited bool

	ForceSendFECOnIdlePath bool

	SchedulingScheme     protocol.SchedulingSchemeID
	SchedulingSchemeName string

	CongestionControl     protocol.CongestionControlID
	CongestionControlName string
}

// A Listener for incoming QUIC connections
type Listener interface {
	// Close the server, sending CONNECTION_CLOSE frames to each peer.
	Close() error
	// Addr returns the local network addr that the server is listening on.
	Addr() net.Addr
	// Accept returns new sessions. It should be called in a loop.
	Accept() (Session, error)
}

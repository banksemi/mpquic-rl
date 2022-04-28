package quic

import (
	"bytes"
	"github.com/lucas-clemente/quic-go/fec"
	"math"
	"time"

	"github.com/lucas-clemente/quic-go/ackhandler"
	"github.com/lucas-clemente/quic-go/congestion"
	"github.com/lucas-clemente/quic-go/internal/flowcontrol"
	"github.com/lucas-clemente/quic-go/internal/handshake"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/wire"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

type mockSealer struct{}

func (s *mockSealer) Seal(dst, src []byte, packetNumber protocol.PacketNumber, associatedData []byte) []byte {
	return append(src, bytes.Repeat([]byte{0}, 12)...)
}

func (s *mockSealer) Overhead() int { return 12 }

var _ handshake.Sealer = &mockSealer{}

type mockCryptoSetup struct {
	handleErr          error
	divNonce           []byte
	encLevelSeal       protocol.EncryptionLevel
	encLevelSealCrypto protocol.EncryptionLevel
	nextPacketType     protocol.PacketType
}

var _ handshake.CryptoSetup = &mockCryptoSetup{}

func (m *mockCryptoSetup) HandleCryptoStream() error {
	return m.handleErr
}
func (m *mockCryptoSetup) Open(dst, src []byte, packetNumber protocol.PacketNumber, associatedData []byte) ([]byte, protocol.EncryptionLevel, error) {
	return nil, protocol.EncryptionUnspecified, nil
}
func (m *mockCryptoSetup) GetSealer() (protocol.EncryptionLevel, handshake.Sealer) {
	return m.encLevelSeal, &mockSealer{}
}
func (m *mockCryptoSetup) GetSealerForCryptoStream() (protocol.EncryptionLevel, handshake.Sealer) {
	return m.encLevelSealCrypto, &mockSealer{}
}
func (m *mockCryptoSetup) GetSealerWithEncryptionLevel(protocol.EncryptionLevel) (handshake.Sealer, error) {
	return &mockSealer{}, nil
}
func (m *mockCryptoSetup) DiversificationNonce() []byte            { return m.divNonce }
func (m *mockCryptoSetup) SetDiversificationNonce(divNonce []byte) { m.divNonce = divNonce }
func (m *mockCryptoSetup) GetNextPacketType() protocol.PacketType  { return m.nextPacketType }

var _ = Describe("Packet packer", func() {
	var (
		packer                *packetPacker
		publicHeaderLen       protocol.ByteCount
		maxFrameSize          protocol.ByteCount
		streamFramer          *streamFramer
		cryptoStream          *stream
		pth                   *path
		fecProtectionOverhead protocol.ByteCount
	)

	BeforeEach(func() {

		headerFECProtected := &wire.Header{
			FECFlag:         true,
			PacketNumberLen: 4,
		}
		headerNonFECProtected := &wire.Header{
			FECFlag:         false,
			PacketNumberLen: 4,
		}

		fflen, _ := (&wire.FECFrame{}).MinLength(protocol.Version39)
		lengthFECProtected, err := headerFECProtected.GetLength(protocol.PerspectiveServer, protocol.Version39)
		Expect(err).ToNot(HaveOccurred())

		lengthNonFECProtected, err := headerNonFECProtected.GetLength(protocol.PerspectiveServer, protocol.Version39)
		Expect(err).ToNot(HaveOccurred())

		fecProtectionOverhead = lengthFECProtected - lengthNonFECProtected + fflen + 25
		cryptoStream = &stream{flowController: flowcontrol.NewStreamFlowController(1, false, flowcontrol.NewConnectionFlowController(1000, 1000, nil, make(map[protocol.PathID]time.Duration)), 1000, 1000, 1000, nil, make(map[protocol.PathID]time.Duration))}

		newStream2 := func(id protocol.StreamID) streamI {
			return newStream(id, nil, nil, nil, protocol.VersionWhatever)
		}

		// We need this when we create frames on new streams
		streamsMap := newStreamsMap(newStream2, protocol.PerspectiveServer, protocol.VersionWhatever)
		streamFramer = newStreamFramer(cryptoStream, streamsMap, nil, false)

		pth = &path{
			sentPacketHandler:     ackhandler.NewSentPacketHandler(&congestion.RTTStats{}, nil, nil, nil, nil, false),
			packetNumberGenerator: newPacketNumberGenerator(protocol.SkipPacketAveragePeriodLength),
			rttStats:              &congestion.RTTStats{},
		}

		sess := &session{version: protocol.Version39, perspective: protocol.PerspectiveClient, recoveredPackets: make(chan *receivedPacket, 10), config: &Config{}}
		fecFramer := newFECFramer(sess, protocol.VersionWhatever)
		rc := fec.NewConstantRedundancyController(10, 1, 1, 1)
		sess.fecFrameworkSender = NewFECFrameworkSender(&fec.XORFECScheme{}, fec.NewRoundRobinScheduler(rc, protocol.Version39), fecFramer, rc, protocol.Version39)
		sess.fecFrameworkReceiver = NewFECFrameworkReceiver(sess, &fec.XORFECScheme{})
		packer = &packetPacker{
			cryptoSetup:  &mockCryptoSetup{encLevelSeal: protocol.EncryptionForwardSecure},
			connectionID: 0x1337,
			streamFramer: streamFramer,
			perspective:  protocol.PerspectiveServer,
			stopWaiting:  make(map[protocol.PathID]*wire.StopWaitingFrame),
			ackFrame:     make(map[protocol.PathID]*wire.AckFrame),
			fecFramer:    fecFramer,
			sess:         sess,
		}
		publicHeaderLen = 1 + 8 + 2 // 1 flag byte, 8 connection ID, 2 packet number
		maxFrameSize = protocol.MaxPacketSize - protocol.ByteCount((&mockSealer{}).Overhead()) - publicHeaderLen
		packer.version = protocol.VersionWhatever
	})

	It("returns nil when no packet is queued", func() {
		p, err := packer.PackPacket(pth, 0)
		Expect(p).To(BeNil())
		Expect(err).ToNot(HaveOccurred())
	})

	It("packs single packets", func() {
		f := &wire.StreamFrame{
			StreamID: 5,
			Data:     []byte{0xDE, 0xCA, 0xFB, 0xAD},
		}
		streamFramer.AddFrameForRetransmission(f)
		p, err := packer.PackPacket(pth, 0)
		Expect(err).ToNot(HaveOccurred())
		Expect(p).ToNot(BeNil())
		b := &bytes.Buffer{}
		f.Write(b, 0)
		Expect(p.frames).To(HaveLen(1))
		Expect(p.raw).To(ContainSubstring(string(b.Bytes())))
	})

	It("stores the encryption level a packet was sealed with", func() {
		packer.cryptoSetup.(*mockCryptoSetup).encLevelSeal = protocol.EncryptionForwardSecure
		f := &wire.StreamFrame{
			StreamID: 5,
			Data:     []byte("foobar"),
		}
		streamFramer.AddFrameForRetransmission(f)
		p, err := packer.PackPacket(pth, 0)
		Expect(err).ToNot(HaveOccurred())
		Expect(p.encryptionLevel).To(Equal(protocol.EncryptionForwardSecure))
	})

	Context("generating a packet header", func() {
		const (
			versionPublicHeader = protocol.Version39  // a QUIC version that uses the Public Header format
			versionIETFHeader   = protocol.VersionTLS // a QUIC version taht uses the IETF Header format
		)

		Context("Public Header (for gQUIC)", func() {
			BeforeEach(func() {
				packer.version = versionPublicHeader
			})

			It("it omits the connection ID for forward-secure packets", func() {
				ph := packer.getHeader(protocol.EncryptionForwardSecure, pth)
				Expect(ph.OmitConnectionID).To(BeFalse())
				packer.SetOmitConnectionID()
				ph = packer.getHeader(protocol.EncryptionForwardSecure, pth)
				Expect(ph.OmitConnectionID).To(BeTrue())
			})

			It("doesn't omit the connection ID for non-forward-secure packets", func() {
				packer.SetOmitConnectionID()
				ph := packer.getHeader(protocol.EncryptionSecure, pth)
				Expect(ph.OmitConnectionID).To(BeFalse())
			})

			It("adds the Version Flag to the Public Header before the crypto handshake is finished", func() {
				packer.perspective = protocol.PerspectiveClient
				ph := packer.getHeader(protocol.EncryptionSecure, pth)
				Expect(ph.VersionFlag).To(BeTrue())
			})

			It("doesn't add the Version Flag to the Public Header for forward-secure packets", func() {
				packer.perspective = protocol.PerspectiveClient
				ph := packer.getHeader(protocol.EncryptionForwardSecure, pth)
				Expect(ph.VersionFlag).To(BeFalse())
			})

			Context("diversificaton nonces", func() {
				var nonce []byte

				BeforeEach(func() {
					nonce = bytes.Repeat([]byte{'e'}, 32)
					packer.cryptoSetup.(*mockCryptoSetup).divNonce = nonce
				})

				It("doesn't include a div nonce, when sending a packet with initial encryption", func() {
					ph := packer.getHeader(protocol.EncryptionUnencrypted, pth)
					Expect(ph.DiversificationNonce).To(BeEmpty())
				})

				It("includes a div nonce, when sending a packet with secure encryption", func() {
					ph := packer.getHeader(protocol.EncryptionSecure, pth)
					Expect(ph.DiversificationNonce).To(Equal(nonce))
				})

				It("doesn't include a div nonce, when sending a packet with forward-secure encryption", func() {
					ph := packer.getHeader(protocol.EncryptionForwardSecure, pth)
					Expect(ph.DiversificationNonce).To(BeEmpty())
				})

				It("doesn't send a div nonce as a client", func() {
					packer.perspective = protocol.PerspectiveClient
					ph := packer.getHeader(protocol.EncryptionSecure, pth)
					Expect(ph.DiversificationNonce).To(BeEmpty())
				})
			})
		})

		Context("Header (for IETF draft QUIC)", func() {
			BeforeEach(func() {
				packer.version = versionIETFHeader
			})

			It("uses the Long Header format for non-forward-secure packets", func() {
				h := packer.getHeader(protocol.EncryptionSecure, pth)
				Expect(h.IsLongHeader).To(BeTrue())
				Expect(h.PacketNumberLen).To(Equal(protocol.PacketNumberLen4))
				Expect(h.Version).To(Equal(versionIETFHeader))
			})

			It("sets the packet type based on the state of the handshake", func() {
				packer.cryptoSetup.(*mockCryptoSetup).nextPacketType = 5
				h := packer.getHeader(protocol.EncryptionSecure, pth)
				Expect(h.IsLongHeader).To(BeTrue())
				Expect(h.Type).To(Equal(protocol.PacketType(5)))
			})

			It("uses the Short Header format for forward-secure packets", func() {
				h := packer.getHeader(protocol.EncryptionForwardSecure, pth)
				Expect(h.IsLongHeader).To(BeFalse())
				Expect(h.PacketNumberLen).To(BeNumerically(">", 0))
			})

			It("it omits the connection ID for forward-secure packets", func() {
				h := packer.getHeader(protocol.EncryptionForwardSecure, pth)
				Expect(h.OmitConnectionID).To(BeFalse())
				packer.SetOmitConnectionID()
				h = packer.getHeader(protocol.EncryptionForwardSecure, pth)
				Expect(h.OmitConnectionID).To(BeTrue())
			})

			It("doesn't omit the connection ID for non-forward-secure packets", func() {
				packer.SetOmitConnectionID()
				h := packer.getHeader(protocol.EncryptionSecure, pth)
				Expect(h.OmitConnectionID).To(BeFalse())
			})
		})
	})

	It("packs a ConnectionClose", func() {
		ccf := wire.ConnectionCloseFrame{
			ErrorCode:    0x1337,
			ReasonPhrase: "foobar",
		}
		p, err := packer.PackConnectionClose(&ccf, pth)
		Expect(err).ToNot(HaveOccurred())
		Expect(p.frames).To(HaveLen(1))
		Expect(p.frames[0]).To(Equal(&ccf))
	})

	It("doesn't send any other frames when sending a ConnectionClose", func() {
		ccf := wire.ConnectionCloseFrame{
			ErrorCode:    0x1337,
			ReasonPhrase: "foobar",
		}
		packer.controlFrames = []wire.Frame{&wire.MaxStreamDataFrame{StreamID: 37}}
		streamFramer.AddFrameForRetransmission(&wire.StreamFrame{
			StreamID: 5,
			Data:     []byte("foobar"),
		})
		p, err := packer.PackConnectionClose(&ccf, pth)
		Expect(err).ToNot(HaveOccurred())
		Expect(p.frames).To(HaveLen(1))
		Expect(p.frames[0]).To(Equal(&ccf))
	})

	It("packs only control frames", func() {
		packer.QueueControlFrame(&wire.RstStreamFrame{}, pth)
		packer.QueueControlFrame(&wire.MaxDataFrame{}, pth)
		p, err := packer.PackPacket(pth, 0)
		Expect(p).ToNot(BeNil())
		Expect(err).ToNot(HaveOccurred())
		Expect(p.frames).To(HaveLen(2))
		Expect(p.raw).NotTo(BeEmpty())
	})

	It("increases the packet number", func() {
		packer.QueueControlFrame(&wire.RstStreamFrame{}, pth)
		p1, err := packer.PackPacket(pth, 0)
		Expect(err).ToNot(HaveOccurred())
		Expect(p1).ToNot(BeNil())
		packer.QueueControlFrame(&wire.RstStreamFrame{}, pth)
		p2, err := packer.PackPacket(pth, 0)
		Expect(err).ToNot(HaveOccurred())
		Expect(p2).ToNot(BeNil())
		Expect(p2.header.PacketNumber).To(BeNumerically(">", p1.header.PacketNumber))
	})

	It("packs a StopWaitingFrame first", func() {
		pth.packetNumberGenerator.next = 15
		swf := &wire.StopWaitingFrame{LeastUnacked: 10}
		packer.QueueControlFrame(&wire.RstStreamFrame{}, pth)
		packer.QueueControlFrame(swf, pth)
		p, err := packer.PackPacket(pth, 0)
		Expect(err).ToNot(HaveOccurred())
		Expect(p).ToNot(BeNil())
		Expect(p.frames).To(HaveLen(2))
		Expect(p.frames[0]).To(Equal(swf))
	})

	It("sets the LeastUnackedDelta length of a StopWaitingFrame", func() {
		packetNumber := protocol.PacketNumber(0xDECAFB) // will result in a 4 byte packet number
		pth.packetNumberGenerator.next = packetNumber
		swf := &wire.StopWaitingFrame{LeastUnacked: packetNumber - 0x100}
		packer.QueueControlFrame(&wire.RstStreamFrame{}, pth)
		packer.QueueControlFrame(swf, pth)
		p, err := packer.PackPacket(pth, 0)
		Expect(err).ToNot(HaveOccurred())
		Expect(p.frames[0].(*wire.StopWaitingFrame).PacketNumberLen).To(Equal(protocol.PacketNumberLen4))
	})

	It("does not pack a packet containing only a StopWaitingFrame", func() {
		swf := &wire.StopWaitingFrame{LeastUnacked: 10}
		packer.QueueControlFrame(swf, pth)
		p, err := packer.PackPacket(pth, 0)
		Expect(p).To(BeNil())
		Expect(err).ToNot(HaveOccurred())
	})

	It("packs a packet if it has queued control frames, but no new control frames", func() {
		packer.controlFrames = []wire.Frame{&wire.BlockedFrame{}}
		p, err := packer.PackPacket(pth, 0)
		Expect(err).ToNot(HaveOccurred())
		Expect(p).ToNot(BeNil())
	})

	It("packs many control frames into 1 packets", func() {
		f := &wire.AckFrame{LargestAcked: 1}
		b := &bytes.Buffer{}
		f.Write(b, protocol.VersionWhatever)
		maxFramesPerPacket := int(maxFrameSize) / b.Len()
		var controlFrames []wire.Frame
		for i := 0; i < maxFramesPerPacket; i++ {
			controlFrames = append(controlFrames, f)
		}
		packer.controlFrames = controlFrames
		payloadFrames, err := packer.composeNextPacket(maxFrameSize, false, pth, fecProtectionOverhead)
		Expect(err).ToNot(HaveOccurred())
		Expect(payloadFrames).To(HaveLen(maxFramesPerPacket))
		payloadFrames, err = packer.composeNextPacket(maxFrameSize, false, pth, fecProtectionOverhead)
		Expect(err).ToNot(HaveOccurred())
		Expect(payloadFrames).To(BeEmpty())
	})

	It("packs a lot of control frames into 2 packets if they don't fit into one", func() {
		blockedFrame := &wire.BlockedFrame{}
		minLength, _ := blockedFrame.MinLength(0)
		maxFramesPerPacket := int(maxFrameSize) / int(minLength)
		var controlFrames []wire.Frame
		for i := 0; i < maxFramesPerPacket+10; i++ {
			controlFrames = append(controlFrames, blockedFrame)
		}
		packer.controlFrames = controlFrames
		payloadFrames, err := packer.composeNextPacket(maxFrameSize, false, pth, fecProtectionOverhead)
		Expect(err).ToNot(HaveOccurred())
		Expect(payloadFrames).To(HaveLen(maxFramesPerPacket))
		payloadFrames, err = packer.composeNextPacket(maxFrameSize, false, pth, fecProtectionOverhead)
		Expect(err).ToNot(HaveOccurred())
		Expect(payloadFrames).To(HaveLen(10))
	})

	It("only increases the packet number when there is an actual packet to send", func() {
		pth.packetNumberGenerator.nextToSkip = 1000
		p, err := packer.PackPacket(pth, 0)
		Expect(p).To(BeNil())
		Expect(err).ToNot(HaveOccurred())
		Expect(pth.packetNumberGenerator.Peek()).To(Equal(protocol.PacketNumber(1)))
		f := &wire.StreamFrame{
			StreamID: 5,
			Data:     []byte{0xDE, 0xCA, 0xFB, 0xAD},
		}
		streamFramer.AddFrameForRetransmission(f)
		p, err = packer.PackPacket(pth, 0)
		Expect(err).ToNot(HaveOccurred())
		Expect(p).ToNot(BeNil())
		Expect(p.header.PacketNumber).To(Equal(protocol.PacketNumber(1)))
		Expect(pth.packetNumberGenerator.Peek()).To(Equal(protocol.PacketNumber(2)))
	})

	Context("Stream Frame handling", func() {
		It("does not splits a stream frame with maximum size", func() {
			f := &wire.StreamFrame{
				Offset:         1,
				StreamID:       5,
				DataLenPresent: false,
			}
			minLength, _ := f.MinLength(0)
			maxStreamFrameDataLen := maxFrameSize - minLength
			f.Data = bytes.Repeat([]byte{'f'}, int(maxStreamFrameDataLen))
			streamFramer.AddFrameForRetransmission(f)
			payloadFrames, err := packer.composeNextPacket(maxFrameSize, true, pth, fecProtectionOverhead)
			Expect(err).ToNot(HaveOccurred())
			Expect(payloadFrames).To(HaveLen(1))
			Expect(payloadFrames[0].(*wire.StreamFrame).DataLenPresent).To(BeFalse())
			payloadFrames, err = packer.composeNextPacket(maxFrameSize, true, pth, fecProtectionOverhead)
			Expect(err).ToNot(HaveOccurred())
			Expect(payloadFrames).To(BeEmpty())
		})

		It("correctly handles a stream frame with one byte less than maximum size", func() {
			maxStreamFrameDataLen := maxFrameSize - (1 + 1 + 2 + 1) - 1 // Also path ID
			f1 := &wire.StreamFrame{
				StreamID: 5,
				Offset:   1,
				Data:     bytes.Repeat([]byte{'f'}, int(maxStreamFrameDataLen)),
			}
			f2 := &wire.StreamFrame{
				StreamID: 5,
				Offset:   1,
				Data:     []byte("foobar"),
			}
			streamFramer.AddFrameForRetransmission(f1)
			streamFramer.AddFrameForRetransmission(f2)
			p, err := packer.PackPacket(pth, 0)
			Expect(err).ToNot(HaveOccurred())
			Expect(p.raw).To(HaveLen(int(protocol.MaxPacketSize - 1)))
			Expect(p.frames).To(HaveLen(1))
			Expect(p.frames[0].(*wire.StreamFrame).DataLenPresent).To(BeFalse())
			p, err = packer.PackPacket(pth, 0)
			Expect(err).ToNot(HaveOccurred())
			Expect(p.frames).To(HaveLen(1))
			Expect(p.frames[0].(*wire.StreamFrame).DataLenPresent).To(BeFalse())
		})

		It("packs multiple small stream frames into single packet", func() {
			f1 := &wire.StreamFrame{
				StreamID: 5,
				Data:     []byte{0xDE, 0xCA, 0xFB, 0xAD},
			}
			f2 := &wire.StreamFrame{
				StreamID: 5,
				Data:     []byte{0xBE, 0xEF, 0x13, 0x37},
			}
			f3 := &wire.StreamFrame{
				StreamID: 3,
				Data:     []byte{0xCA, 0xFE},
			}
			streamFramer.AddFrameForRetransmission(f1)
			streamFramer.AddFrameForRetransmission(f2)
			streamFramer.AddFrameForRetransmission(f3)
			p, err := packer.PackPacket(pth, 0)
			Expect(p).ToNot(BeNil())
			Expect(err).ToNot(HaveOccurred())
			b := &bytes.Buffer{}
			f1.Write(b, 0)
			f2.Write(b, 0)
			f3.Write(b, 0)
			Expect(p.frames).To(HaveLen(3))
			Expect(p.frames[0].(*wire.StreamFrame).DataLenPresent).To(BeTrue())
			Expect(p.frames[1].(*wire.StreamFrame).DataLenPresent).To(BeTrue())
			Expect(p.frames[2].(*wire.StreamFrame).DataLenPresent).To(BeFalse())
			Expect(p.raw).To(ContainSubstring(string(f1.Data)))
			Expect(p.raw).To(ContainSubstring(string(f2.Data)))
			Expect(p.raw).To(ContainSubstring(string(f3.Data)))
		})

		It("splits one stream frame larger than maximum size", func() {
			f := &wire.StreamFrame{
				StreamID: 7,
				Offset:   1,
			}
			minLength, _ := f.MinLength(0)
			maxStreamFrameDataLen := maxFrameSize - minLength
			f.Data = bytes.Repeat([]byte{'f'}, int(maxStreamFrameDataLen)+200)
			streamFramer.AddFrameForRetransmission(f)
			payloadFrames, err := packer.composeNextPacket(maxFrameSize, true, pth, fecProtectionOverhead)
			Expect(err).ToNot(HaveOccurred())
			Expect(payloadFrames).To(HaveLen(1))
			Expect(payloadFrames[0].(*wire.StreamFrame).DataLenPresent).To(BeFalse())
			Expect(payloadFrames[0].(*wire.StreamFrame).Data).To(HaveLen(int(maxStreamFrameDataLen)))
			payloadFrames, err = packer.composeNextPacket(maxFrameSize, true, pth, fecProtectionOverhead)
			Expect(err).ToNot(HaveOccurred())
			Expect(payloadFrames).To(HaveLen(1))
			Expect(payloadFrames[0].(*wire.StreamFrame).Data).To(HaveLen(200))
			Expect(payloadFrames[0].(*wire.StreamFrame).DataLenPresent).To(BeFalse())
			payloadFrames, err = packer.composeNextPacket(maxFrameSize, true, pth, fecProtectionOverhead)
			Expect(err).ToNot(HaveOccurred())
			Expect(payloadFrames).To(BeEmpty())
		})

		It("packs 2 stream frames that are too big for one packet correctly", func() {
			maxStreamFrameDataLen := maxFrameSize - (1 + 1 + 2)
			f1 := &wire.StreamFrame{
				StreamID: 5,
				Data:     bytes.Repeat([]byte{'f'}, int(maxStreamFrameDataLen)+100),
				Offset:   1,
			}
			f2 := &wire.StreamFrame{
				StreamID: 5,
				Data:     bytes.Repeat([]byte{'f'}, int(maxStreamFrameDataLen)+100),
				Offset:   1,
			}
			streamFramer.AddFrameForRetransmission(f1)
			streamFramer.AddFrameForRetransmission(f2)
			p, err := packer.PackPacket(pth, 0)
			Expect(err).ToNot(HaveOccurred())
			Expect(p.frames).To(HaveLen(1))
			Expect(p.frames[0].(*wire.StreamFrame).DataLenPresent).To(BeFalse())
			Expect(p.raw).To(HaveLen(int(protocol.MaxPacketSize)))
			p, err = packer.PackPacket(pth, 0)
			Expect(p.frames).To(HaveLen(2))
			Expect(p.frames[0].(*wire.StreamFrame).DataLenPresent).To(BeTrue())
			Expect(p.frames[1].(*wire.StreamFrame).DataLenPresent).To(BeFalse())
			Expect(err).ToNot(HaveOccurred())
			Expect(p.header.FECFlag).To(BeFalse())
			Expect(p.raw).To(HaveLen(int(protocol.MaxPacketSize)))
			p, err = packer.PackPacket(pth, 0)
			Expect(p.frames).To(HaveLen(1))
			Expect(p.frames[0].(*wire.StreamFrame).DataLenPresent).To(BeFalse())
			Expect(err).ToNot(HaveOccurred())
			Expect(p).ToNot(BeNil())
			p, err = packer.PackPacket(pth, 0)
			Expect(err).ToNot(HaveOccurred())
			Expect(p).To(BeNil())
		})

		It("packs a packet that has the maximum packet size when given a large enough stream frame", func() {
			f := &wire.StreamFrame{
				StreamID: 5,
				Offset:   1,
			}
			minLength, _ := f.MinLength(0)
			f.Data = bytes.Repeat([]byte{'f'}, int(maxFrameSize-minLength+1)) // + 1 since MinceLength is 1 bigger than the actual StreamFrame header
			streamFramer.AddFrameForRetransmission(f)
			p, err := packer.PackPacket(pth, 0)
			Expect(err).ToNot(HaveOccurred())
			Expect(p).ToNot(BeNil())
			Expect(p.raw).To(HaveLen(int(protocol.MaxPacketSize)))
		})

		It("splits a stream frame larger than the maximum size", func() {
			f := &wire.StreamFrame{
				StreamID: 5,
				Offset:   1,
			}
			minLength, _ := f.MinLength(0)
			f.Data = bytes.Repeat([]byte{'f'}, int(maxFrameSize-minLength+2)) // + 2 since MinceLength is 1 bigger than the actual StreamFrame header

			streamFramer.AddFrameForRetransmission(f)
			payloadFrames, err := packer.composeNextPacket(maxFrameSize, true, pth, fecProtectionOverhead)
			Expect(err).ToNot(HaveOccurred())
			Expect(payloadFrames).To(HaveLen(1))
			payloadFrames, err = packer.composeNextPacket(maxFrameSize, true, pth, fecProtectionOverhead)
			Expect(err).ToNot(HaveOccurred())
			Expect(payloadFrames).To(HaveLen(1))
		})

		It("refuses to send unencrypted stream data on a data stream", func() {
			packer.cryptoSetup.(*mockCryptoSetup).encLevelSeal = protocol.EncryptionUnencrypted
			f := &wire.StreamFrame{
				StreamID: 3,
				Data:     []byte("foobar"),
			}
			streamFramer.AddFrameForRetransmission(f)
			p, err := packer.PackPacket(pth, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(p).To(BeNil())
		})

		It("sends non forward-secure data as the client", func() {
			packer.perspective = protocol.PerspectiveClient
			packer.cryptoSetup.(*mockCryptoSetup).encLevelSeal = protocol.EncryptionSecure
			f := &wire.StreamFrame{
				StreamID: 5,
				Data:     []byte("foobar"),
			}
			streamFramer.AddFrameForRetransmission(f)
			p, err := packer.PackPacket(pth, 0)
			Expect(err).ToNot(HaveOccurred())
			Expect(p.encryptionLevel).To(Equal(protocol.EncryptionSecure))
			Expect(p.frames[0]).To(Equal(f))
		})

		It("does not send non forward-secure data as the server", func() {
			packer.cryptoSetup.(*mockCryptoSetup).encLevelSeal = protocol.EncryptionSecure
			f := &wire.StreamFrame{
				StreamID: 5,
				Data:     []byte("foobar"),
			}
			streamFramer.AddFrameForRetransmission(f)
			p, err := packer.PackPacket(pth, 0)
			Expect(err).ToNot(HaveOccurred())
			Expect(p).To(BeNil())
		})

		It("sends unencrypted stream data on the crypto stream", func() {
			packer.cryptoSetup.(*mockCryptoSetup).encLevelSealCrypto = protocol.EncryptionUnencrypted
			cryptoStream.dataForWriting = []byte("foobar")
			p, err := packer.PackPacket(pth, 0)
			Expect(err).ToNot(HaveOccurred())
			Expect(p.encryptionLevel).To(Equal(protocol.EncryptionUnencrypted))
			Expect(p.frames).To(HaveLen(1))
			Expect(p.frames[0]).To(Equal(&wire.StreamFrame{
				StreamID: packer.version.CryptoStreamID(),
				Data:     []byte("foobar"),
			}))
		})

		It("sends encrypted stream data on the crypto stream", func() {
			packer.cryptoSetup.(*mockCryptoSetup).encLevelSealCrypto = protocol.EncryptionSecure
			cryptoStream.dataForWriting = []byte("foobar")
			p, err := packer.PackPacket(pth, 0)
			Expect(err).ToNot(HaveOccurred())
			Expect(p.encryptionLevel).To(Equal(protocol.EncryptionSecure))
			Expect(p.frames).To(HaveLen(1))
			Expect(p.frames[0]).To(Equal(&wire.StreamFrame{
				StreamID: packer.version.CryptoStreamID(),
				Data:     []byte("foobar"),
			}))
		})

		It("does not pack stream frames if not allowed", func() {
			packer.cryptoSetup.(*mockCryptoSetup).encLevelSeal = protocol.EncryptionUnencrypted
			packer.QueueControlFrame(&wire.AckFrame{}, pth)
			streamFramer.AddFrameForRetransmission(&wire.StreamFrame{StreamID: 3, Data: []byte("foobar")})
			p, err := packer.PackPacket(pth, 0)
			Expect(err).ToNot(HaveOccurred())
			Expect(p.frames).To(HaveLen(1))
			Expect(func() { _ = p.frames[0].(*wire.AckFrame) }).NotTo(Panic())
		})
	})

	Context("Blocked frames", func() {
		It("queues a BLOCKED frame", func() {
			length := 100
			streamFramer.blockedFrameQueue = []wire.Frame{&wire.StreamBlockedFrame{StreamID: 5}}
			f := &wire.StreamFrame{
				StreamID: 5,
				Data:     bytes.Repeat([]byte{'f'}, length),
			}
			streamFramer.AddFrameForRetransmission(f)
			_, err := packer.composeNextPacket(maxFrameSize, true, pth, fecProtectionOverhead)
			Expect(err).ToNot(HaveOccurred())
			Expect(packer.controlFrames[0]).To(Equal(&wire.StreamBlockedFrame{StreamID: 5}))
		})

		It("removes the dataLen attribute from the last StreamFrame, even if it queued a BLOCKED frame", func() {
			length := 100
			streamFramer.blockedFrameQueue = []wire.Frame{&wire.StreamBlockedFrame{StreamID: 5}}
			f := &wire.StreamFrame{
				StreamID: 5,
				Data:     bytes.Repeat([]byte{'f'}, length),
			}
			streamFramer.AddFrameForRetransmission(f)
			p, err := packer.composeNextPacket(maxFrameSize, true, pth, fecProtectionOverhead)
			Expect(err).ToNot(HaveOccurred())
			Expect(p).To(HaveLen(1))
			Expect(p[0].(*wire.StreamFrame).DataLenPresent).To(BeFalse())
		})

		It("packs a connection-level BlockedFrame", func() {
			streamFramer.blockedFrameQueue = []wire.Frame{&wire.BlockedFrame{}}
			f := &wire.StreamFrame{
				StreamID: 5,
				Data:     []byte("foobar"),
			}
			streamFramer.AddFrameForRetransmission(f)
			_, err := packer.composeNextPacket(maxFrameSize, true, pth, fecProtectionOverhead)
			Expect(err).ToNot(HaveOccurred())
			Expect(packer.controlFrames[0]).To(Equal(&wire.BlockedFrame{}))
		})
	})

	It("returns nil if we only have a single STOP_WAITING", func() {
		packer.QueueControlFrame(&wire.StopWaitingFrame{}, pth)
		p, err := packer.PackPacket(pth, 0)
		Expect(err).NotTo(HaveOccurred())
		Expect(p).To(BeNil())
	})

	It("packs a single ACK", func() {
		ack := &wire.AckFrame{LargestAcked: 42}
		packer.QueueControlFrame(ack, pth)
		p, err := packer.PackPacket(pth, 0)
		Expect(err).NotTo(HaveOccurred())
		Expect(p).ToNot(BeNil())
		Expect(p.frames[0]).To(Equal(ack))
	})

	It("does not return nil if we only have a single ACK but request it to be sent", func() {
		ack := &wire.AckFrame{}
		packer.QueueControlFrame(ack, pth)
		p, err := packer.PackPacket(pth, 0)
		Expect(err).NotTo(HaveOccurred())
		Expect(p).ToNot(BeNil())
	})

	It("queues a control frame to be sent in the next packet", func() {
		msd := &wire.MaxStreamDataFrame{StreamID: 5}
		packer.QueueControlFrame(msd, pth)
		p, err := packer.PackPacket(pth, 0)
		Expect(err).NotTo(HaveOccurred())
		Expect(p.frames).To(HaveLen(1))
		Expect(p.frames[0]).To(Equal(msd))
	})

	Context("retransmitting of handshake packets", func() {
		swf := &wire.StopWaitingFrame{LeastUnacked: 1}
		sf := &wire.StreamFrame{
			StreamID: 1,
			Data:     []byte("foobar"),
		}

		BeforeEach(func() {
			packer.QueueControlFrame(swf, pth)
		})

		It("packs a retransmission for a packet sent with no encryption", func() {
			packet := &ackhandler.Packet{
				EncryptionLevel: protocol.EncryptionUnencrypted,
				Frames:          []wire.Frame{sf},
			}
			p, err := packer.PackHandshakeRetransmission(packet, pth)
			Expect(err).ToNot(HaveOccurred())
			Expect(p.frames).To(ContainElement(sf))
			Expect(p.frames).To(ContainElement(swf))
			Expect(p.encryptionLevel).To(Equal(protocol.EncryptionUnencrypted))
		})

		It("packs a retransmission for a packet sent with initial encryption", func() {
			nonce := bytes.Repeat([]byte{'e'}, 32)
			packer.cryptoSetup.(*mockCryptoSetup).divNonce = nonce
			packet := &ackhandler.Packet{
				EncryptionLevel: protocol.EncryptionSecure,
				Frames:          []wire.Frame{sf},
			}
			p, err := packer.PackHandshakeRetransmission(packet, pth)
			Expect(err).ToNot(HaveOccurred())
			Expect(p.frames).To(ContainElement(sf))
			Expect(p.frames).To(ContainElement(swf))
			Expect(p.encryptionLevel).To(Equal(protocol.EncryptionSecure))
			// a packet sent by the server with initial encryption contains the SHLO
			// it needs to have a diversification nonce
			Expect(p.raw).To(ContainSubstring(string(nonce)))
		})

		It("includes the diversification nonce on packets sent with initial encryption", func() {
			packet := &ackhandler.Packet{
				EncryptionLevel: protocol.EncryptionSecure,
				Frames:          []wire.Frame{sf},
			}
			p, err := packer.PackHandshakeRetransmission(packet, pth)
			Expect(err).ToNot(HaveOccurred())
			Expect(p.encryptionLevel).To(Equal(protocol.EncryptionSecure))
		})

		// this should never happen, since non forward-secure packets are limited to a size smaller than MaxPacketSize, such that it is always possible to retransmit them without splitting the StreamFrame
		// (note that the retransmitted packet needs to have enough space for the StopWaitingFrame)
		It("refuses to send a packet larger than MaxPacketSize", func() {
			packet := &ackhandler.Packet{
				EncryptionLevel: protocol.EncryptionSecure,
				Frames: []wire.Frame{
					&wire.StreamFrame{
						StreamID: 1,
						Data:     bytes.Repeat([]byte{'f'}, int(protocol.MaxPacketSize-5)),
					},
				},
			}
			_, err := packer.PackHandshakeRetransmission(packet, pth)
			Expect(err).To(MatchError(ContainSubstring("PacketPacker BUG: packet too large")))
		})

		It("refuses to retransmit packets that were sent with forward-secure encryption", func() {
			p := &ackhandler.Packet{
				EncryptionLevel: protocol.EncryptionForwardSecure,
			}
			_, err := packer.PackHandshakeRetransmission(p, pth)
			Expect(err).To(MatchError("PacketPacker BUG: forward-secure encrypted handshake packets don't need special treatment"))
		})

		It("refuses to retransmit packets without a StopWaitingFrame", func() {
			packer.stopWaiting = nil
			_, err := packer.PackHandshakeRetransmission(&ackhandler.Packet{
				EncryptionLevel: protocol.EncryptionSecure,
			}, pth)
			Expect(err).To(MatchError("PacketPacker BUG: Handshake retransmissions must contain a StopWaitingFrame"))
		})
	})

	Context("packing ACK packets", func() {
		It("packs ACK packets", func() {
			packer.QueueControlFrame(&wire.AckFrame{}, pth)
			p, err := packer.PackAckPacket(pth)
			Expect(err).NotTo(HaveOccurred())
			Expect(p.frames).To(Equal([]wire.Frame{&wire.AckFrame{DelayTime: math.MaxInt64}}))
		})

		It("packs ACK packets with SWFs", func() {
			packer.QueueControlFrame(&wire.AckFrame{}, pth)
			packer.QueueControlFrame(&wire.StopWaitingFrame{}, pth)
			p, err := packer.PackAckPacket(pth)
			Expect(err).NotTo(HaveOccurred())
			Expect(p.frames).To(Equal([]wire.Frame{
				&wire.AckFrame{DelayTime: math.MaxInt64},
				&wire.StopWaitingFrame{PacketNumber: 1, PacketNumberLen: 2},
			}))
		})
	})

	It("packs PING packet", func() {
		p, err := packer.PackPing(&wire.PingFrame{}, pth)
		Expect(err).NotTo(HaveOccurred())
		Expect(p.frames).To(Equal([]wire.Frame{
			&wire.PingFrame{},
		}))
	})
})

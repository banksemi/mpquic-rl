package quic

import (
	"errors"
	"github.com/lucas-clemente/quic-go/fec"
	"net"
	"sync"
	"time"

	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/wire"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

type mockPconnManager struct {
	addedAddrID   chan protocol.AddressID
	removedAddrID chan protocol.AddressID
	localAddrs    map[protocol.AddressID]*net.UDPAddr
	mutex         sync.RWMutex
	pconns        map[protocol.AddressID]net.PacketConn
	ifNames       map[int]string
	addrIDsOfAddr map[string]protocol.AddressID
}

func (mpc *mockPconnManager) AddedAddrIDChan() chan protocol.AddressID        { return mpc.addedAddrID }
func (mpc *mockPconnManager) RemovedAddrIDChan() chan protocol.AddressID      { return mpc.removedAddrID }
func (mpc *mockPconnManager) LocalAddrs() map[protocol.AddressID]*net.UDPAddr { return mpc.localAddrs }
func (mpc *mockPconnManager) Pconns() map[protocol.AddressID]net.PacketConn   { return mpc.pconns }
func (mpc *mockPconnManager) PconnsLock() *sync.RWMutex                       { return &mpc.mutex }
func (mpc *mockPconnManager) GetAddrIDOf(addr net.Addr) (protocol.AddressID, bool) {
	addrID, ok := mpc.addrIDsOfAddr[addr.String()]
	return addrID, ok
}
func (mpc *mockPconnManager) GetPreferredPort() int { return 0 }
func (mpc *mockPconnManager) GetInterfaceName(addr net.UDPAddr) (string, bool) {
	for k, c := range mpc.pconns {
		a, _ := net.ResolveUDPAddr("udp", c.LocalAddr().String())
		if addr.IP.Equal(a.IP) {
			return mpc.ifNames[int(k)], true
		}
	}
	return "", false
}

var _ pconnManagerI = &mockPconnManager{}

var _ = Describe("Path Manager", func() {
	var c connection
	var pconn *mockPacketConn
	var pconnMgr *mockPconnManager
	var sess *mockSession
	var pm *pathManager
	var remoteAddr *net.UDPAddr

	BeforeEach(func() {
		pconnMgr = &mockPconnManager{
			addedAddrID:   make(chan protocol.AddressID, 1),
			removedAddrID: make(chan protocol.AddressID, 1),
			localAddrs:    make(map[protocol.AddressID]*net.UDPAddr),
			pconns:        make(map[protocol.AddressID]net.PacketConn),
			addrIDsOfAddr: make(map[string]protocol.AddressID),
		}
		pconnMgr.localAddrs[0] = &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 0x1337}
		pconn = &mockPacketConn{addr: pconnMgr.localAddrs[0]}
		remoteAddr = &net.UDPAddr{IP: net.IPv4(7, 6, 5, 4), Port: 443}
		sess = &mockSession{
			config:       &Config{},
			paths:        make(map[protocol.PathID]*path),
			streamFramer: newStreamFramer(nil, nil, nil, false),
		}
		pm = &pathManager{pconnMgr: pconnMgr, sess: sess}
		c = &conn{
			currentAddr: remoteAddr,
			pconn:       pconn,
		}
		pm.setup(c, fec.NewConstantRedundancyController(10, 1, 1, 1))
	})

	Context("before handshake completed", func() {
		It("creates the initial path correctly", func() {
			Expect(len(sess.paths)).To(Equal(1))
			Expect(sess.paths[0].conn.LocalAddr().String()).To(Equal(pconnMgr.localAddrs[0].String()))
			Expect(sess.paths[0].conn.RemoteAddr().String()).To(Equal(remoteAddr.String()))
		})

		It("does not create any path other than the initial one", func() {
			pconnMgr.localAddrs[1] = &net.UDPAddr{IP: net.IPv4(5, 6, 7, 8), Port: 0x7331}
			pconnMgr.addedAddrID <- 1
			Consistently(func() int { return len(sess.paths) }).Should(Equal(1))
		})
	})

	Context("when handshake completed", func() {
		BeforeEach(func() {
			// Ok, the connection began
			pm.handshakeCompleted <- struct{}{}
		})

		Context("with server perspective", func() {
			BeforeEach(func() {
				sess.perspective = protocol.PerspectiveServer
			})

			It("does not create any path by itself", func() {
				sess.maxPathID = 2
				pconnMgr.PconnsLock().Lock()
				pconnMgr.localAddrs[1] = &net.UDPAddr{IP: net.IPv4(5, 6, 7, 8), Port: 0x7331}
				pconnMgr.pconns[1] = &mockPacketConn{addr: pconnMgr.localAddrs[1]}
				pconnMgr.PconnsLock().Unlock()
				pconnMgr.addedAddrID <- 1

				pconnMgr.PconnsLock().Lock()
				pconnMgr.localAddrs[2] = &net.UDPAddr{IP: net.IPv4(5, 7, 6, 12), Port: 0x1234}
				pconnMgr.pconns[2] = &mockPacketConn{addr: pconnMgr.localAddrs[2]}
				pconnMgr.PconnsLock().Unlock()
				pconnMgr.addedAddrID <- 2

				pconnMgr.PconnsLock().Lock()
				pconnMgr.localAddrs[3] = &net.UDPAddr{IP: net.IPv4(78, 17, 68, 172), Port: 0x1010}
				pconnMgr.pconns[3] = &mockPacketConn{addr: pconnMgr.localAddrs[3]}
				pconnMgr.PconnsLock().Unlock()
				pconnMgr.addedAddrID <- 3

				Consistently(func() bool { return len(sess.paths) > 1 }).Should(BeFalse())
			})

			It("queues as many ADD_ADDRESS frames than there are local addresses", func() {
				pconnMgr.PconnsLock().Lock()
				pconnMgr.localAddrs[1] = &net.UDPAddr{IP: net.IPv4(5, 6, 7, 8), Port: 0x7331}
				pconnMgr.pconns[1] = &mockPacketConn{addr: pconnMgr.localAddrs[1]}
				pconnMgr.PconnsLock().Unlock()
				pconnMgr.addedAddrID <- 1
				pconnMgr.PconnsLock().Lock()
				pconnMgr.localAddrs[2] = &net.UDPAddr{IP: net.IPv4(5, 7, 6, 12), Port: 0x1234}
				pconnMgr.pconns[2] = &mockPacketConn{addr: pconnMgr.localAddrs[2]}
				pconnMgr.PconnsLock().Unlock()
				pconnMgr.addedAddrID <- 2
				pconnMgr.PconnsLock().Lock()
				pconnMgr.localAddrs[3] = &net.UDPAddr{IP: net.IPv4(78, 17, 68, 172), Port: 0x1010}
				pconnMgr.pconns[3] = &mockPacketConn{addr: pconnMgr.localAddrs[3]}
				pconnMgr.PconnsLock().Unlock()
				pconnMgr.addedAddrID <- 3

				Eventually(func() int { return len(sess.GetStreamFramer().addAddressFrameQueue) }).Should(Equal(4)) // The first one + 3 others
			})

			It("queues an ADD_ADDRESS frame with backup bit set in handover mode for a cellular address", func() {
				sess.config.MultipathService = Handover
				pconnMgr.ifNames = map[int]string{
					1: "rmnet0",
				}

				// Get the first ADD_ADDRESS
				Eventually(func() int { return len(sess.GetStreamFramer().addAddressFrameQueue) }).Should(Equal(1))
				Expect(sess.GetStreamFramer().addAddressFrameQueue[0].Backup).To(BeFalse())

				pconnMgr.PconnsLock().Lock()
				pconnMgr.localAddrs[1] = &net.UDPAddr{IP: net.IPv4(5, 6, 7, 8), Port: 0x7331}
				pconnMgr.pconns[1] = &mockPacketConn{addr: pconnMgr.localAddrs[1]}
				pconnMgr.PconnsLock().Unlock()
				pconnMgr.addedAddrID <- 1

				Eventually(func() int { return len(sess.GetStreamFramer().addAddressFrameQueue) }).Should(Equal(2)) // The first one + 3 others
				Expect(sess.GetStreamFramer().addAddressFrameQueue[1].Backup).To(BeTrue())
			})

			It("queues an ADD_ADDRESS frame without backup bit set in aggregate mode for a cellular address", func() {
				sess.config.MultipathService = Aggregate
				pconnMgr.ifNames = map[int]string{
					1: "rmnet0",
				}
				pconnMgr.PconnsLock().Lock()
				pconnMgr.localAddrs[1] = &net.UDPAddr{IP: net.IPv4(5, 6, 7, 8), Port: 0x7331}
				pconnMgr.pconns[1] = &mockPacketConn{addr: pconnMgr.localAddrs[1]}
				pconnMgr.PconnsLock().Unlock()
				pconnMgr.addedAddrID <- 1

				Eventually(func() int { return len(sess.GetStreamFramer().addAddressFrameQueue) }).Should(Equal(2)) // The first one + 3 others
				Expect(sess.GetStreamFramer().addAddressFrameQueue[1].Backup).To(BeFalse())
			})

			It("does not queue ADD_ADDRESS frames for non global unicast local addresses", func() {
				pconnMgr.PconnsLock().Lock()
				pconnMgr.localAddrs[1] = &net.UDPAddr{IP: net.IPv4(0, 0, 0, 0), Port: 0x7331}
				pconnMgr.pconns[1] = &mockPacketConn{addr: pconnMgr.localAddrs[1]}
				pconnMgr.PconnsLock().Unlock()
				pconnMgr.addedAddrID <- 1
				pconnMgr.PconnsLock().Lock()
				pconnMgr.localAddrs[2] = &net.UDPAddr{IP: net.ParseIP("::"), Port: 0x1234}
				pconnMgr.pconns[2] = &mockPacketConn{addr: pconnMgr.localAddrs[2]}
				pconnMgr.PconnsLock().Unlock()
				pconnMgr.addedAddrID <- 2
				pconnMgr.PconnsLock().Lock()
				pconnMgr.localAddrs[3] = &net.UDPAddr{IP: net.ParseIP("fe80::a0c2:e921:1284:9876"), Port: 0x1234}
				pconnMgr.pconns[3] = &mockPacketConn{addr: pconnMgr.localAddrs[3]}
				pconnMgr.PconnsLock().Unlock()
				pconnMgr.addedAddrID <- 3

				Eventually(func() int { return len(sess.GetStreamFramer().addAddressFrameQueue) }).Should(Equal(1))
				Consistently(func() bool { return len(sess.GetStreamFramer().addAddressFrameQueue) > 1 }).Should(BeFalse()) // Only the first one
			})

			It("creates a path when a new path ID is provided", func() {
				sess.maxPathID = 2
				p := &receivedPacket{
					remoteAddr: remoteAddr,
					header:     &wire.Header{PathID: 0x1},
					rcvTime:    time.Now(),
					rcvPconn:   pconn,
				}
				pth, err := pm.createPathFromRemote(p)

				Expect(err).NotTo(HaveOccurred())
				Eventually(func() int { return len(sess.paths) }).Should(Equal(2))
				Expect(pth).NotTo(BeNil())
				Expect(pth.pathID).To(Equal(protocol.PathID(0x1)))
			})

			It("does no create a new path with already used path ID", func() {
				sess.maxPathID = 2
				p := &receivedPacket{
					remoteAddr: remoteAddr,
					header:     &wire.Header{PathID: 0x0},
					rcvTime:    time.Now(),
					rcvPconn:   pconn,
				}
				pth, err := pm.createPathFromRemote(p)

				Expect(pth).To(BeNil())
				Expect(err).To(MatchError(errors.New("trying to create already existing path")))
				Consistently(func() bool { return len(sess.paths) > 1 }).Should(BeFalse())
			})

			It("does not create a path when a new path ID greater than maxPathID is provided", func() {
				sess.maxPathID = 0
				p := &receivedPacket{
					remoteAddr: remoteAddr,
					header:     &wire.Header{PathID: 0x1},
					rcvTime:    time.Now(),
					rcvPconn:   pconn,
				}
				pth, err := pm.createPathFromRemote(p)

				Expect(pth).To(BeNil())
				Expect(err).To(MatchError(errors.New("path ID greater than maxPathID")))
				Consistently(func() bool { return len(sess.paths) > 1 }).Should(BeFalse())
			})

			It("sets correctly the remote addr ID and backup status if a PATHS frame arrived before the path was created", func() {
				sess.maxPathID = 2
				remoteAddr2 := &net.UDPAddr{IP: net.IPv4(45, 45, 45, 45), Port: 1234}
				pm.handleAddAddressFrame(&wire.AddAddressFrame{AddrID: 1, Addr: *remoteAddr2, Backup: true})

				// ADD_ADDRESS has been processed
				Eventually(func() string { return pm.remoteAddrs[1].String() }).Should(Equal(remoteAddr2.String()))

				// FIXME this mimics the PATHS frame, should be done in session when receiving a PATHS with unknown path ID
				pm.remoteAddrIDOfComingPaths[protocol.PathID(1)] = protocol.AddressID(1)

				p := &receivedPacket{
					remoteAddr: remoteAddr2,
					header:     &wire.Header{PathID: 0x1},
					rcvTime:    time.Now(),
					rcvPconn:   pconn,
				}
				pth, err := pm.createPathFromRemote(p)
				Expect(err).To(BeNil())
				Expect(pth.remAddrID).To(Equal(protocol.AddressID(1)))
				Expect(pth.backup.Get()).To(BeTrue())
			})
		})

		Context("with client perspective", func() {
			BeforeEach(func() {
				sess.perspective = protocol.PerspectiveClient
			})

			It("creates new paths when it has enough addresses", func() {
				sess.maxPathID = 1
				pconnMgr.PconnsLock().Lock()
				pconnMgr.localAddrs[1] = &net.UDPAddr{IP: net.IPv4(5, 6, 7, 8), Port: 0x7331}
				pconnMgr.pconns[1] = &mockPacketConn{addr: pconnMgr.localAddrs[1]}
				pconnMgr.PconnsLock().Unlock()
				pconnMgr.addedAddrID <- 1

				Eventually(func() int { return len(sess.paths) }).Should(Equal(2))
				Expect(sess.paths[1].conn.LocalAddr().String()).To(Equal(pconnMgr.localAddrs[1].String()))
				Expect(sess.paths[1].conn.RemoteAddr().String()).To(Equal(remoteAddr.String()))
				Expect(sess.paths[1].locAddrID).To(Equal(protocol.AddressID(1)))
				Expect(sess.paths[1].remAddrID).To(Equal(protocol.AddressID(0)))
				Expect(pm.remoteAddrs[0].String()).To(Equal(remoteAddr.String()))
				Expect(sess.paths[0].conn.LocalAddr().String()).To(Equal(pconnMgr.localAddrs[0].String()))
				Expect(sess.paths[0].conn.RemoteAddr().String()).To(Equal(remoteAddr.String()))
				Expect(sess.paths[0].locAddrID).To(Equal(protocol.AddressID(0)))
				Expect(sess.paths[0].remAddrID).To(Equal(protocol.AddressID(0)))
			})

			It("does not create more paths than the maxPathID", func() {
				sess.maxPathID = 2
				pconnMgr.PconnsLock().Lock()
				pconnMgr.localAddrs[1] = &net.UDPAddr{IP: net.IPv4(5, 6, 7, 8), Port: 0x7331}
				pconnMgr.pconns[1] = &mockPacketConn{addr: pconnMgr.localAddrs[1]}
				pconnMgr.PconnsLock().Unlock()
				pconnMgr.addedAddrID <- 1
				pconnMgr.PconnsLock().Lock()
				pconnMgr.localAddrs[2] = &net.UDPAddr{IP: net.IPv4(5, 7, 6, 12), Port: 0x1234}
				pconnMgr.pconns[2] = &mockPacketConn{addr: pconnMgr.localAddrs[2]}
				pconnMgr.PconnsLock().Unlock()
				pconnMgr.addedAddrID <- 2
				pconnMgr.PconnsLock().Lock()
				pconnMgr.localAddrs[3] = &net.UDPAddr{IP: net.IPv4(78, 17, 68, 172), Port: 0x1010}
				pconnMgr.pconns[3] = &mockPacketConn{addr: pconnMgr.localAddrs[3]}
				pconnMgr.PconnsLock().Unlock()
				pconnMgr.addedAddrID <- 3

				Eventually(func() int { return len(sess.paths) }).Should(Equal(3))
				Consistently(func() bool { return len(sess.paths) > 3 }).Should(BeFalse())
			})

			It("records remote addresses coming from ADD_ADDRESS frames and tries to start using it", func() {
				sess.maxPathID = 2
				remoteAddr2 := net.UDPAddr{IP: net.IPv4(45, 45, 45, 45), Port: 1234}
				pm.handleAddAddressFrame(&wire.AddAddressFrame{AddrID: 1, Addr: remoteAddr2})

				Eventually(func() int { return len(sess.paths) }).Should(Equal(2))
				Consistently(func() bool { return len(sess.paths) > 2 }).Should(BeFalse())
				Expect(pm.remoteAddrs[1].String()).To(Equal(remoteAddr2.String()))
				Expect(sess.paths[1].locAddrID).To(Equal(protocol.AddressID(0)))
				Expect(sess.paths[1].remAddrID).To(Equal(protocol.AddressID(1)))
			})

			It("removes remote address given by a REMOVE_ADDRESS and disable the path if no more addresses are available", func() {
				sess.maxPathID = 1
				remoteAddr2 := net.UDPAddr{IP: net.IPv4(45, 45, 45, 45), Port: 1234}
				pm.handleAddAddressFrame(&wire.AddAddressFrame{AddrID: 7, Addr: remoteAddr2})

				Eventually(func() int { return len(pm.remoteAddrs) }).Should(Equal(2))

				Expect(sess.paths[1].remAddrID).To(Equal(protocol.AddressID(7)))

				pm.handleRemoveAddressFrame(&wire.RemoveAddressFrame{AddrID: 7})
				Eventually(func() bool { return sess.paths[1].active.Get() }).Should(BeFalse())
				Eventually(func() bool { return sess.paths[1].validRemAddrID }).Should(BeFalse())
			})

			It("tries to move an unactive path towards another functional remote address", func() {
				sess.maxPathID = 1
				remoteAddr2 := net.UDPAddr{IP: net.IPv4(45, 45, 45, 45), Port: 1234}
				pm.handleAddAddressFrame(&wire.AddAddressFrame{AddrID: 7, Addr: remoteAddr2})

				Eventually(func() int { return len(pm.remoteAddrs) }).Should(Equal(2))
				Eventually(func() int { return len(sess.paths) }).Should(Equal(2))
				Expect(sess.paths[1].remAddrID).To(Equal(protocol.AddressID(7)))

				remoteAddr3 := net.UDPAddr{IP: net.IPv4(78, 68, 91, 74), Port: 5678}
				pm.handleAddAddressFrame(&wire.AddAddressFrame{AddrID: 9, Addr: remoteAddr3})

				Eventually(func() int { return len(pm.remoteAddrs) }).Should(Equal(3))

				pm.handleRemoveAddressFrame(&wire.RemoveAddressFrame{AddrID: 7})
				Eventually(func() int { return len(pm.remoteAddrs) }).Should(Equal(2))
				Eventually(func() protocol.AddressID { return sess.paths[1].remAddrID }).Should(Equal(protocol.AddressID(9)))
				Eventually(func() bool { return sess.paths[1].active.Get() }).Should(BeTrue())
				Eventually(func() bool { return sess.paths[1].validRemAddrID }).Should(BeTrue())
			})

			It("stops using the path if the local address is no more available and schedules REMOVE_ADDRESS frame", func() {
				sess.maxPathID = 1
				pconnMgr.PconnsLock().Lock()
				pconnMgr.localAddrs[1] = &net.UDPAddr{IP: net.IPv4(5, 6, 7, 8), Port: 0x7331}
				pconnMgr.pconns[1] = &mockPacketConn{addr: pconnMgr.localAddrs[1]}
				pconnMgr.PconnsLock().Unlock()
				pconnMgr.addedAddrID <- 1

				Eventually(func() int { return len(sess.paths) }).Should(Equal(2))
				Consistently(func() bool { return len(sess.paths) > 2 }).Should(BeFalse())

				// Remove the local addr 0
				delete(pconnMgr.localAddrs, 0)
				delete(pconnMgr.pconns, 0)
				pconnMgr.removedAddrID <- 0

				//Eventually(func() int { return len(pm.reusablePaths) }).Should(Equal(1))
				Eventually(func() bool { return sess.paths[0].active.Get() }).Should(BeFalse())
				Eventually(func() bool { return sess.paths[0].validRemAddrID }).Should(BeFalse())
				Eventually(func() connection { return sess.paths[0].conn }).Should(BeNil())
				Eventually(func() int { return len(sess.GetStreamFramer().removeAddressFrameQueue) }).Should(Equal(1))
				Expect(sess.GetStreamFramer().removeAddressFrameQueue[0].AddrID).To(Equal(protocol.AddressID(0)))
			})

			It("tries to reuse on another local address a path that lost its local address", func() {
				sess.maxPathID = 1
				pconnMgr.PconnsLock().Lock()
				pconnMgr.localAddrs[1] = &net.UDPAddr{IP: net.IPv4(5, 6, 7, 8), Port: 0x7331}
				pconnMgr.pconns[1] = &mockPacketConn{addr: pconnMgr.localAddrs[1]}
				pconnMgr.PconnsLock().Unlock()
				pconnMgr.addedAddrID <- 1

				Eventually(func() int { return len(sess.paths) }).Should(Equal(2))
				Consistently(func() bool { return len(sess.paths) > 2 }).Should(BeFalse())

				// New remote address
				remoteAddr2 := net.UDPAddr{IP: net.IPv4(45, 45, 45, 45), Port: 1234}
				pm.handleAddAddressFrame(&wire.AddAddressFrame{AddrID: 7, Addr: remoteAddr2})

				// Remove the local addr 0
				delete(pconnMgr.localAddrs, 0)
				delete(pconnMgr.pconns, 0)
				pconnMgr.removedAddrID <- 0

				//Eventually(func() int { return len(pm.reusablePaths) }).Should(Equal(1))
				Eventually(func() protocol.AddressID { return sess.paths[0].locAddrID }).Should(Equal(protocol.AddressID(1)))
				Eventually(func() protocol.AddressID { return sess.paths[0].remAddrID }).Should(Equal(protocol.AddressID(7)))
				Expect(sess.paths[0].active.Get()).To(BeTrue())
			})

			It("avoids creating additional paths using the anycast address", func() {
				sess.maxPathID = 255
				remoteAddr2 := net.UDPAddr{IP: net.ParseIP("2001:db0:c:d::1"), Port: 1234}
				pm.handleAddAddressFrame(&wire.AddAddressFrame{AddrID: 1, Addr: remoteAddr2})
				pconnMgr.PconnsLock().Lock()
				pconnMgr.localAddrs[1] = &net.UDPAddr{IP: net.IPv4(0, 0, 0, 0), Port: 0x7331}
				pconnMgr.pconns[1] = &mockPacketConn{addr: pconnMgr.localAddrs[1]}
				pconnMgr.PconnsLock().Unlock()
				pconnMgr.addedAddrID <- 1
				pconnMgr.PconnsLock().Lock()
				pconnMgr.localAddrs[2] = &net.UDPAddr{IP: net.ParseIP("::"), Port: 0x1234}
				pconnMgr.pconns[2] = &mockPacketConn{addr: pconnMgr.localAddrs[2]}
				pconnMgr.PconnsLock().Unlock()
				pconnMgr.addedAddrID <- 2
				pconnMgr.PconnsLock().Lock()
				pconnMgr.localAddrs[3] = &net.UDPAddr{IP: net.ParseIP("fe80::a0c2:e921:1284:9876"), Port: 0x1234}
				pconnMgr.pconns[3] = &mockPacketConn{addr: pconnMgr.localAddrs[3]}
				pconnMgr.PconnsLock().Unlock()
				pconnMgr.addedAddrID <- 3
				pconnMgr.PconnsLock().Lock()
				pconnMgr.localAddrs[4] = &net.UDPAddr{IP: net.ParseIP("fd07:6042:7878:0:a0c2:e921:1284:9876"), Port: 0x1234}
				pconnMgr.pconns[4] = &mockPacketConn{addr: pconnMgr.localAddrs[4]}
				pconnMgr.PconnsLock().Unlock()
				pconnMgr.addedAddrID <- 4

				Eventually(func() int { return len(sess.paths) }).Should(Equal(1))
				Consistently(func() bool { return len(sess.paths) > 1 }).Should(BeFalse())
			})

			It("creates backup paths on cellular interfaces in handover mode", func() {
				sess.maxPathID = 255
				remoteAddr2 := net.UDPAddr{IP: net.ParseIP("2001:db0:c:d::1"), Port: 1234}
				pm.handleAddAddressFrame(&wire.AddAddressFrame{AddrID: 1, Addr: remoteAddr2})
				sess.config.MultipathService = Handover
				pconnMgr.ifNames = map[int]string{
					1: "rmnet0",
					2: "pdp_ip0",
					3: "rmnet1",
				}
				pconnMgr.PconnsLock().Lock()
				pconnMgr.localAddrs[1] = &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 0x7331}
				pconnMgr.pconns[1] = &mockPacketConn{addr: pconnMgr.localAddrs[1]}
				pconnMgr.PconnsLock().Unlock()
				pconnMgr.addedAddrID <- 1
				pconnMgr.PconnsLock().Lock()
				pconnMgr.localAddrs[2] = &net.UDPAddr{IP: net.ParseIP("2001:db0:a:b::1"), Port: 0x1234}
				pconnMgr.pconns[2] = &mockPacketConn{addr: pconnMgr.localAddrs[2]}
				pconnMgr.PconnsLock().Unlock()
				pconnMgr.addedAddrID <- 2
				pconnMgr.PconnsLock().Lock()
				pconnMgr.localAddrs[3] = &net.UDPAddr{IP: net.ParseIP("2001:db0:b:b::1"), Port: 0x1234}
				pconnMgr.pconns[3] = &mockPacketConn{addr: pconnMgr.localAddrs[3]}
				pconnMgr.PconnsLock().Unlock()
				pconnMgr.addedAddrID <- 3

				Eventually(func() int { return len(sess.paths) }).Should(Equal(4))
				Consistently(func() bool { return len(sess.paths) > 4 }).Should(BeFalse())
				Expect(sess.paths[1].backup.Get()).To(BeTrue())
				Expect(sess.paths[2].backup.Get()).To(BeTrue())
				Expect(sess.paths[3].backup.Get()).To(BeTrue())
			})

			It("creates non-backup paths on cellular interfaces in aggregate mode", func() {
				sess.maxPathID = 255
				remoteAddr2 := net.UDPAddr{IP: net.ParseIP("2001:db0:c:d::1"), Port: 1234}
				pm.handleAddAddressFrame(&wire.AddAddressFrame{AddrID: 1, Addr: remoteAddr2})
				sess.config.MultipathService = Aggregate
				pconnMgr.ifNames = map[int]string{
					1: "rmnet0",
					2: "pdp_ip0",
					3: "rmnet1",
				}
				pconnMgr.PconnsLock().Lock()
				pconnMgr.localAddrs[1] = &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 0x7331}
				pconnMgr.pconns[1] = &mockPacketConn{addr: pconnMgr.localAddrs[1]}
				pconnMgr.PconnsLock().Unlock()
				pconnMgr.addedAddrID <- 1
				pconnMgr.PconnsLock().Lock()
				pconnMgr.localAddrs[2] = &net.UDPAddr{IP: net.ParseIP("2001:db0:a:b::1"), Port: 0x1234}
				pconnMgr.pconns[2] = &mockPacketConn{addr: pconnMgr.localAddrs[2]}
				pconnMgr.PconnsLock().Unlock()
				pconnMgr.addedAddrID <- 2
				pconnMgr.PconnsLock().Lock()
				pconnMgr.localAddrs[3] = &net.UDPAddr{IP: net.ParseIP("2001:db0:b:b::1"), Port: 0x1234}
				pconnMgr.pconns[3] = &mockPacketConn{addr: pconnMgr.localAddrs[3]}
				pconnMgr.PconnsLock().Unlock()
				pconnMgr.addedAddrID <- 3

				Eventually(func() int { return len(sess.paths) }).Should(Equal(4))
				Consistently(func() bool { return len(sess.paths) > 4 }).Should(BeFalse())
				Expect(sess.paths[1].backup.Get()).To(BeFalse())
				Expect(sess.paths[2].backup.Get()).To(BeFalse())
				Expect(sess.paths[3].backup.Get()).To(BeFalse())
			})
		})
	})
})

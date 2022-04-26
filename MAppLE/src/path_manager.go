package quic

import (
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/lucas-clemente/quic-go/congestion"
	"github.com/lucas-clemente/quic-go/fec"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
	"github.com/lucas-clemente/quic-go/internal/wire"
	"github.com/lucas-clemente/quic-go/sedpf"
)

type pathManager struct {
	pconnMgr  pconnManagerI
	sess      sessionI
	nxtPathID protocol.PathID

	remoteAddrs   map[protocol.AddressID]*net.UDPAddr
	remoteBackups map[protocol.AddressID]bool

	// List of inactive path IDs
	reusablePaths []protocol.PathID

	advertisedLocAddrs map[protocol.AddressID]bool

	addAddressChange    chan *wire.AddAddressFrame
	removeAddressChange chan *wire.RemoveAddressFrame

	// TODO (QDC): find a cleaner way
	oliaSenders map[protocol.PathID]*congestion.OliaSender

	// It might be possible to receive a PATHS frame describing a path not yet created
	// This map stores the remote Addr ID of such paths
	remoteAddrIDOfComingPaths map[protocol.PathID]protocol.AddressID

	handshakeCompleted chan struct{}
	runClosed          chan struct{}
	timer              *time.Timer

	redundancyController fec.RedundancyController

	// Rendez-vous point when closing all paths
	wg         sync.WaitGroup
	hasFECPath *utils.AtomicBool
}

func (pm *pathManager) setup(conn connection, redundancyController fec.RedundancyController) {
	// Initial PathID is 0
	pm.nxtPathID = 1

	pm.remoteAddrs = make(map[protocol.AddressID]*net.UDPAddr)
	pm.remoteBackups = make(map[protocol.AddressID]bool)
	pm.advertisedLocAddrs = make(map[protocol.AddressID]bool)
	pm.handshakeCompleted = make(chan struct{}, 1)
	pm.addAddressChange = make(chan *wire.AddAddressFrame, 1)
	pm.removeAddressChange = make(chan *wire.RemoveAddressFrame, 1)
	pm.runClosed = make(chan struct{}, 1)
	pm.timer = time.NewTimer(0)
	pm.reusablePaths = make([]protocol.PathID, 0)
	pm.redundancyController = redundancyController

	pm.oliaSenders = make(map[protocol.PathID]*congestion.OliaSender)

	pm.remoteAddrIDOfComingPaths = make(map[protocol.PathID]protocol.AddressID)

	pm.hasFECPath = &utils.AtomicBool{}

	paths := pm.sess.Paths()

	// Setup the first path of the connection
	paths[protocol.InitialPathID] = &path{
		pathID: protocol.InitialPathID,
		sess:   pm.sess,
		conn:   conn,
	}

	// Setup this first path
	paths[protocol.InitialPathID].setup(pm.oliaSenders, pm.redundancyController)
	pm.wg.Add(1)

	// With the initial path, get the remoteAddr to create paths accordingly
	if conn.RemoteAddr() != nil {
		remAddr, err := net.ResolveUDPAddr("udp", conn.RemoteAddr().String())
		if err != nil {
			utils.Errorf("path manager: encountered error while parsing remote addr: %v", remAddr)
		}

		pm.remoteAddrs[0] = remAddr
	}

	// Launch the path manager
	go pm.run()
}

func (pm *pathManager) tryCreatingPaths() {
	if pm.sess.GetPerspective() == protocol.PerspectiveServer {
		pm.advertiseAddresses()
	}
	if pm.canCreatePaths() {
		pm.createPaths()
	}
}

func (pm *pathManager) addRemoteAddress(f *wire.AddAddressFrame) {
	pm.sess.PathsLock().Lock()
	defer pm.sess.PathsLock().Unlock()

	pm.remoteAddrs[f.AddrID] = &f.Addr
	pm.remoteBackups[f.AddrID] = f.Backup
	// Particular case: if address ID 0 is actually the one announced, perform the change
	if pm.remoteAddrs[protocol.AddressID(0)].String() == pm.remoteAddrs[f.AddrID].String() {
		for _, pth := range pm.sess.Paths() {
			if pth.remAddrID == protocol.AddressID(0) && pth.validRemAddrID {
				pth.remAddrID = f.AddrID
				bk := f.Backup
				if pm.sess.GetConfig().MultipathService == Handover {
					bk = bk || pm.isBackupAddress(pth.locAddrID)
				}
				pth.backup.Set(bk)
			}
		}
		delete(pm.remoteAddrs, protocol.AddressID(0))
	}
}

func (pm *pathManager) removeRemoteAddress(f *wire.RemoveAddressFrame) {
	if _, ok := pm.remoteAddrs[f.AddrID]; ok {
		delete(pm.remoteAddrs, f.AddrID)
		delete(pm.remoteBackups, f.AddrID)
	}

	pm.sess.PathsLock().Lock()
	defer pm.sess.PathsLock().Unlock()
	for pathID, pth := range pm.sess.Paths() {
		if pth.remAddrID == f.AddrID && pth.validRemAddrID {
			pth.active.Set(false)
			pth.validRemAddrID = false
			pm.reusablePaths = append(pm.reusablePaths, pathID)
		}
	}
}

func (pm *pathManager) removeLocalAddress(addrID protocol.AddressID) {
	utils.Debugf("Received remove local address")
	pm.sess.PathsLock().Lock()
	defer pm.sess.PathsLock().Unlock()
	for pathID, pth := range pm.sess.Paths() {
		if pth.locAddrID == addrID {
			pth.active.Set(false)
			pth.validRemAddrID = false
			pth.conn = nil
			pm.reusablePaths = append(pm.reusablePaths, pathID)
		}
	}
	pm.sess.GetStreamFramer().AddRemoveAddressForTransmission(addrID)
}

func (pm *pathManager) run() {

initialLoop:
	for {
		select {
		case <-pm.runClosed:
			// Close immediately if requested
			return
		case addAddressFrame := <-pm.addAddressChange:
			pm.addRemoteAddress(addAddressFrame)
		case removeAddressFrame := <-pm.removeAddressChange:
			pm.removeRemoteAddress(removeAddressFrame)
		case <-pm.pconnMgr.AddedAddrIDChan():
		case addrID := <-pm.pconnMgr.RemovedAddrIDChan():
			pm.removeLocalAddress(addrID)
		case <-pm.handshakeCompleted:
			break initialLoop
		}
	}

	// Handshake completed, try to create paths
	pm.advertiseAddresses()
	if pm.canCreatePaths() {
		err := pm.createPaths()
		if err != nil {
			pm.closePaths()
			return
		}
	}

runLoop:
	for {
		select {
		case <-pm.runClosed:
			break runLoop
		case addAddressFrame := <-pm.addAddressChange:
			pm.addRemoteAddress(addAddressFrame)
		case removeAddressFrame := <-pm.removeAddressChange:
			pm.removeRemoteAddress(removeAddressFrame)
		case <-pm.pconnMgr.AddedAddrIDChan():
		case addrID := <-pm.pconnMgr.RemovedAddrIDChan():
			pm.removeLocalAddress(addrID)
		}
		pm.advertiseAddresses()
		pm.tryCreatingPaths()
	}
}

func (pm *pathManager) canCreatePaths() bool {
	isClient := pm.sess.GetPerspective() == protocol.PerspectiveClient
	hasReusablePaths := len(pm.reusablePaths) > 0
	hasFreePathID := pm.nxtPathID != 0 && pm.nxtPathID <= pm.sess.GetMaxPathID()
	return isClient && (hasReusablePaths || hasFreePathID)
}

func (pm *pathManager) advertiseAddresses() {
	pm.pconnMgr.PconnsLock().RLock()
	defer pm.pconnMgr.PconnsLock().RUnlock()
	localAddrs := pm.pconnMgr.LocalAddrs()
	for addrID, locAddr := range localAddrs {
		_, sent := pm.advertisedLocAddrs[addrID]
		if !sent {
			// Only advertise reachable addresses
			if locAddr.IP.IsGlobalUnicast() {
				advAddr := &net.UDPAddr{IP: locAddr.IP, Port: locAddr.Port}
				if pm.pconnMgr.GetPreferredPort() != 0 {
					advAddr.Port = pm.pconnMgr.GetPreferredPort()
				}
				backup := false
				// Check if it is a backup path or not
				cfg := pm.sess.GetConfig()
				if cfg.MultipathService == Handover {
					ifName, ok := pm.pconnMgr.GetInterfaceName(*locAddr)
					if ok && (strings.HasPrefix(ifName, "rmnet") || strings.HasPrefix(ifName, "pdp_ip")) {
						backup = true
					}
				}
				pm.sess.GetStreamFramer().AddAddAddressForTransmission(addrID, *advAddr, backup)
			}
			pm.advertisedLocAddrs[addrID] = true
		}
	}
}

func (pm *pathManager) createPath(locAddrID protocol.AddressID, locAddr net.UDPAddr, remAddrID protocol.AddressID, remAddr net.UDPAddr) error {
	// First check that the path does not exist yet
	paths := pm.sess.Paths()
	for _, pth := range paths {
		// Skip non-used paths
		if !pth.active.Get() {
			continue
		}
		if pth.conn.LocalAddr().String() == locAddr.String() && pth.conn.RemoteAddr().String() == remAddr.String() {
			// Path already exists, so don't create it again
			return nil
		}
	}
	var pth *path
	var pathID protocol.PathID
	if len(pm.reusablePaths) > 0 {
		pathID = pm.reusablePaths[0]
		pm.reusablePaths = pm.reusablePaths[1:]
		pth = paths[pathID]
		pth.conn = &conn{pconn: pm.pconnMgr.Pconns()[locAddrID], currentAddr: &remAddr}
		pth.locAddrID = locAddrID
		pth.remAddrID = remAddrID

		pth.setupReusePath(pm.oliaSenders)

		if utils.Debug() {
			utils.Debugf("Reuse path %x on %s to %s", pathID, locAddr.String(), remAddr.String())
		}
	} else {
		pathID = pm.nxtPathID
		// No matching path, so create it
		pth = &path{
			pathID:    pathID,
			sess:      pm.sess,
			conn:      &conn{pconn: pm.pconnMgr.Pconns()[locAddrID], currentAddr: &remAddr},
			locAddrID: locAddrID,
			remAddrID: remAddrID,
		}
		pm.wg.Add(1)
		pth.setup(pm.oliaSenders, pm.redundancyController)
		if utils.Debug() {
			utils.Debugf("Starting path %x on %s to %s", pm.nxtPathID, locAddr.String(), remAddr.String())
		}
		pm.nxtPathID++
	}

	// Check if it is a backup path or not
	cfg := pm.sess.GetConfig()
	bk, ok := pm.remoteBackups[pth.remAddrID]
	if !ok {
		bk = false
	}
	if cfg.MultipathService == Handover {
		bk = bk || pm.isBackupAddress(pth.locAddrID)
	}
	pth.backup.Set(bk)

	// use a FEC paths if we already have two other non-fec paths
	if protocol.USE_FEC_DEDICATED_PATH && !bk && !pm.hasFECPath.Get() && len(paths) > 2 {
		// FIXME: do not set a fec path is fec is not needed
		if pth.fec == nil {
			pth.fec = &utils.AtomicBool{}
		}
		pth.fec.Set(true)
		pm.hasFECPath.Set(true)
	}

	// inform S-EDPF scheduler of the existence of the path
	sedpf.AddPath(pathID)

	paths[pathID] = pth
	// Send a PING frame to get latency info about the new path and informing the
	// peer of its existence
	// FIXME PING + PATHS frames
	return pm.sess.SendPing(pth)
}

func (pm *pathManager) isBackupAddress(addrID protocol.AddressID) bool {
	addr := pm.pconnMgr.LocalAddrs()[addrID]
	ifName, ok := pm.pconnMgr.GetInterfaceName(*addr)
	return ok && (strings.HasPrefix(ifName, "rmnet") || strings.HasPrefix(ifName, "pdp_ip"))
}

func (pm *pathManager) createPaths() error {
	if utils.Debug() {
		utils.Debugf("Path manager tries to create paths")
	}

	// TODO (QDC): clearly not optimal
	pm.pconnMgr.PconnsLock().RLock()
	defer pm.pconnMgr.PconnsLock().RUnlock()
	pm.sess.PathsLock().Lock()
	defer pm.sess.PathsLock().Unlock()

	for locAddrID, locAddr := range pm.pconnMgr.LocalAddrs() {
		if (pm.nxtPathID > pm.sess.GetMaxPathID() || pm.nxtPathID == 0) && len(pm.reusablePaths) == 0 {
			break
		}
		// If the local address is an anycast or a link-local one, avoid creating new paths
		if locAddr.IP.Equal(net.IPv4(0, 0, 0, 0)) || locAddr.IP.Equal(net.ParseIP("::")) || locAddr.IP.IsLinkLocalUnicast() || locAddr.IP.IsLinkLocalUnicast() {
			continue
		}
		version := utils.GetIPVersion(locAddr.IP)
		// Don't use global local addresses
		if version == 6 && locAddr.IP[0] >= 0xfd {
			continue
		}
		for remAddrID, remAddr := range pm.remoteAddrs {
			if utils.GetIPVersion(remAddr.IP) != version {
				continue
			}
			err := pm.createPath(locAddrID, *locAddr, remAddrID, *remAddr)
			if err != nil {
				return err
			}
		}
	}
	pm.sess.SchedulePathsFrame()
	return nil
}

func (pm *pathManager) createPathFromRemote(p *receivedPacket) (*path, error) {
	pm.pconnMgr.PconnsLock().RLock()
	defer pm.pconnMgr.PconnsLock().RUnlock()
	pm.sess.PathsLock().Lock()
	defer pm.sess.PathsLock().Unlock()
	localPconn := p.rcvPconn
	remoteAddr := p.remoteAddr
	pathID := p.header.PathID
	paths := pm.sess.Paths()

	// Sanity check: pathID should not exist yet
	_, ko := paths[pathID]
	if ko {
		return nil, errors.New("trying to create already existing path")
	}

	// If we receive a packet with a path ID larger than the maxPathID, it should be dropped
	if pathID > pm.sess.GetMaxPathID() {
		return nil, errors.New("path ID greater than maxPathID")
	}

	pth := &path{
		pathID: pathID,
		sess:   pm.sess,
		conn:   &conn{pconn: localPconn, currentAddr: remoteAddr},
	}

	locAddrID, ok := pm.pconnMgr.GetAddrIDOf(localPconn.LocalAddr())
	if ok {
		pth.locAddrID = locAddrID
	}

	// The remote addr ID could have been communicated before the path creation
	remAddrID, ok := pm.remoteAddrIDOfComingPaths[pathID]
	if ok {
		pth.remAddrID = remAddrID
		delete(pm.remoteAddrIDOfComingPaths, pathID)
	}

	pth.setup(pm.oliaSenders, pm.redundancyController)

	// Check if it is a backup path or not
	cfg := pm.sess.GetConfig()
	bk, ok := pm.remoteBackups[pth.remAddrID]
	if !ok {
		bk = false
	}
	if cfg.MultipathService == Handover {
		bk = bk || pm.isBackupAddress(pth.locAddrID)
	}
	pth.backup.Set(bk)

	// use a FEC paths if we already have two other non-fec paths
	if protocol.USE_FEC_DEDICATED_PATH && !bk && !pm.hasFECPath.Get() && len(paths) > 2 {
		// FIXME: do not set a fec path is fec is not needed
		if pth.fec == nil {
			pth.fec = &utils.AtomicBool{}
		}
		pth.fec.Set(true)
		pm.hasFECPath.Set(true)
	}

	paths[pathID] = pth
	pm.wg.Add(1)

	if utils.Debug() {
		utils.Debugf("Created remote path %x on %s to %s", pathID, localPconn.LocalAddr().String(), remoteAddr.String())
	}

	// inform S-EDPF scheduler of the existence of the path
	sedpf.AddPath(pathID)

	return pth, nil
}

func (pm *pathManager) handleAddAddressFrame(f *wire.AddAddressFrame) {
	pm.addAddressChange <- f
}

func (pm *pathManager) handleRemoveAddressFrame(f *wire.RemoveAddressFrame) {
	pm.removeAddressChange <- f
}

func (pm *pathManager) closePaths() {
	pm.sess.PathsLock().RLock()
	defer pm.sess.PathsLock().RUnlock()
	select {
	case <-pm.runClosed:
		// already closed
		return
	default:
		// continue
	}
	paths := pm.sess.Paths()
	for _, pth := range paths {
		// Independently of active or not paths, close them!
		pth.active.Set(false)
		close(pth.closeChan)
	}
	close(pm.runClosed)
}

// GetNumActivePaths caller MUST hold PathsLock!
func (pm *pathManager) GetNumActivePaths() int {
	return len(pm.sess.Paths()) - len(pm.reusablePaths)
}

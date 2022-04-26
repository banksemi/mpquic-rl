// +build !linux,!darwin

package quic

import (
	"net"
	"strings"
	"sync"
	"time"

	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
)

// netWatcher goes across all interfaces and addresses
type netWatcher struct {
	pconnMgr *pconnManager
	timer    *time.Timer
	lostChan chan struct{}

	// Cache used to detect address changes (netlink would have been nice, but it would only work on Linux...)
	cachedStatus     map[string][]*net.UDPAddr
	cachedStatusLock sync.RWMutex
}

var _ NetWatcherI = &netWatcher{}

func (nw *netWatcher) setup() {
	nw.timer = time.NewTimer(0)
	nw.lostChan = make(chan struct{}, 1)
	nw.cachedStatus = make(map[string][]*net.UDPAddr)

	go nw.run()
}

func (nw *netWatcher) getInterfaceName(addr net.UDPAddr) (string, bool) {
	nw.cachedStatusLock.RLock()
	defer nw.cachedStatusLock.RUnlock()
	for i, _ := range nw.cachedStatus {
		for _, a := range nw.cachedStatus[i] {
			if a.IP.Equal(addr.IP) {
				return i, true
			}
		}
	}
	return "", false
}

func (nw *netWatcher) lostPconn() {
	pcm.closePconn(pconn.LocalAddr())
	nw.lostChan <- struct{}{}
}

func (nw *netWatcher) run() {
	var err error
	if nw.pconnMgr.perspective == protocol.PerspectiveClient {
		err = nw.watchPconns()
		if err != nil {
			utils.Errorf("error in first run of watcher: %v", err)
			return
		}
	}
	// Start the timer for periodic interface checking (only for client)
	duration, _ := time.ParseDuration("500ms")
	if nw.pconnMgr.perspective == protocol.PerspectiveClient {
		nw.timer.Reset(duration)
	} else {
		if !nw.timer.Stop() {
			<-nw.timer.C
		}
	}
runLoop:
	for {
		select {
		case <-nw.pconnMgr.closeConns:
			break runLoop
		case <-nw.timer.C:
			err = nw.watchPconns()
			nw.timer.Reset(duration)
		case <-nw.lostChan:
			err = nw.watchPconns()
		}
		if err != nil {
			utils.Errorf("error in watcher: %v", err)
			break runLoop
		}
	}
	// Close pconns
	nw.pconnMgr.closePconns()
}

func (nw *netWatcher) removeInterfaceAddresses(iName string) {
	nw.cachedStatusLock.Lock()
	defer nw.cachedStatusLock.Unlock()
	// Was it previously available?
	addrs, ok := nw.cachedStatus[iName]
	if ok {
		// Addresses are lost!
		for _, addr := range addrs {
			nw.pconnMgr.closePconn(addr)
		}
		delete(nw.cachedStatus, iName)
	}
}

func (nw *netWatcher) hasValidInterfaceName(iName string) bool {
	return strings.Contains(iName, "eth") || strings.Contains(iName, "rmnet") || strings.Contains(iName, "wlan") || strings.Contains(iName, "en0") || strings.Contains(iName, "pdp_ip0")
}

func (nw *netWatcher) maybeCreateNewPconnOn(iName string, addr net.Addr) (net.IP, error) {
	ip, _, err := net.ParseCIDR(addr.String())
	if err != nil {
		return nil, err
	}
	// Don't consider address as valid if it is not a global unicast address
	if !ip.IsGlobalUnicast() {
		return nil, nil
	}

	// TODO (QDC): Clearly not optimal
	found := false
	nw.pconnMgr.PconnsLock().RLock()
lookingLoop:
	for _, locAddr := range nw.pconnMgr.localAddrs {
		if ip.Equal(locAddr.IP) {
			found = true
			break lookingLoop
		}
	}
	nw.pconnMgr.PconnsLock().RUnlock()
	if !found {
		locAddr, err := nw.pconnMgr.createPconn(ip)
		if err != nil {
			return ip, err
		}
		nw.cachedStatusLock.Lock()
		nw.cachedStatus[iName] = append(nw.cachedStatus[iName], locAddr)
		nw.cachedStatusLock.Unlock()
	}

	return ip, nil
}

func (nw *netWatcher) watchPconns() error {
	ifaces, err := net.Interfaces()
	if err != nil {
		return err
	}
	for _, i := range ifaces {
		// We first need to check if the interface is on
		if !strings.Contains(i.Flags.String(), "up") {
			nw.removeInterfaceAddresses(i.Name)
			continue
		}
		// TODO (QDC): do this in a generic way
		if !nw.hasValidInterfaceName(i.Name) {
			continue
		}
		var seenIPs []net.IP
		addrs, err := i.Addrs()
		if err != nil {
			return err
		}
		// We first look at address additions
		for _, a := range addrs {
			ip, err := nw.maybeCreateNewPconnOn(i.Name, a)
			if err != nil {
				return err
			}
			if ip != nil {
				seenIPs = append(seenIPs, ip)
			}
		}
		// Now detect if an address was lost meanwhile
		// Decrement, to have an easier management of indexing
		nw.cachedStatusLock.Lock()
		for j := len(nw.cachedStatus[i.Name]) - 1; j >= 0; j-- {
			addr := nw.cachedStatus[i.Name][j]
			found := false
		sawLoop:
			for _, sawIP := range seenIPs {
				if addr.IP.Equal(sawIP) {
					found = true
					break sawLoop
				}
			}
			if !found {
				// Address removal detected!
				nw.pconnMgr.closePconn(addr)
				nw.cachedStatus[i.Name] = append(nw.cachedStatus[i.Name][:j], nw.cachedStatus[i.Name][j+1:]...)
			}
		}
		nw.cachedStatusLock.Unlock()
	}
	return nil
}

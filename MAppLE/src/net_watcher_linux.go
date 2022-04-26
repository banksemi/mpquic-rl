// +build linux

package quic

import (
	"net"
	"strings"
	"sync"

	"github.com/lucas-clemente/quic-go/internal/utils"
	"github.com/vishvananda/netlink"
)

// netWatcher leverages NETLINK events on Linux
type netWatcher struct {
	pconnMgr    *pconnManager
	handle      *netlink.Handle
	addrUpdates chan netlink.AddrUpdate
	linkUpdates chan netlink.LinkUpdate
	doneAU      chan struct{}
	doneLU      chan struct{}
	ifAddrs     map[int][]*net.UDPAddr
	ifNames     map[int]string
	mutex       sync.RWMutex
}

var _ NetWatcherI = &netWatcher{}

func (nw *netWatcher) getInterfaceName(addr net.UDPAddr) (string, bool) {
	nw.mutex.RLock()
	defer nw.mutex.RUnlock()
	for i, _ := range nw.ifAddrs {
		for _, a := range nw.ifAddrs[i] {
			if a.IP.Equal(addr.IP) {
				return nw.ifNames[i], true
			}
		}
	}
	return "", false
}

func (nw *netWatcher) lostPconn() {
	// Nothing to do here
}

func (nw *netWatcher) setup() {
	var err error
	nw.handle, err = netlink.NewHandle()
	if err != nil {
		panic(err)
	}

	nw.linkUpdates = make(chan netlink.LinkUpdate, 1)
	nw.addrUpdates = make(chan netlink.AddrUpdate, 1)
	nw.doneAU = make(chan struct{}, 1)
	nw.doneLU = make(chan struct{}, 1)
	nw.ifAddrs = make(map[int][]*net.UDPAddr)
	nw.ifNames = make(map[int]string)

	go nw.run()
}

func notValidIName(iName string) bool {
	return strings.Contains(iName, "docker") || strings.Contains(iName, "tap") || strings.Contains(iName, "tun")
}

func newNetWatcherLinux(manager *pconnManager) NetWatcherI {
	return &netWatcher{
		pconnMgr: manager,
	}
}

func (nw *netWatcher) run() {
	links, err := nw.handle.LinkList()
	if err != nil {
		utils.Errorf("error in first run of watcher: %v", err)
		return
	}
	netlink.AddrSubscribe(nw.addrUpdates, nw.doneAU)
	for _, l := range links {
		iName := l.Attrs().Name
		iIndex := l.Attrs().Index
		if l.Attrs().HardwareAddr != nil && !notValidIName(iName) {
			utils.Infof("netlink init: %v %v %v", iName, iIndex, l.Attrs().HardwareAddr)
			netlink.LinkSubscribe(nw.linkUpdates, nw.doneLU)
			addrs, err := netlink.AddrList(l, 0)
			if err != nil {
				utils.Errorf("error in first run of watcher: %v", err)
				continue
			}
			for _, addr := range addrs {
				if addr.IP == nil || addr.IP.To4() == nil || strings.HasPrefix(addr.IP.String(), "130.75.") {
					// don't use interfaces using the university's WAN addresses
					// or non ipv4 links
					continue
				}
				utils.Infof("%v", addr)
				locAddr, err := nw.pconnMgr.createPconn(addr.IP)
				if err == nil {
					nw.ifAddrs[iIndex] = append(nw.ifAddrs[iIndex], locAddr)
					nw.ifNames[iIndex] = iName
				}
			}
		}
	}
runLoop:
	for {
		select {
		case <-nw.pconnMgr.closeConns:
			break runLoop
		case au := <-nw.addrUpdates:
			if au.NewAddr {
				if au.LinkAddress.IP.To4() == nil {
					// ignore updates for non-ipv4 links
					continue runLoop
				}

				utils.Infof("Netlink event: new addr update %v", au.LinkAddress)
				locAddr, err := nw.pconnMgr.createPconn(au.LinkAddress.IP)
				if err == nil {
					nw.ifAddrs[au.LinkIndex] = append(nw.ifAddrs[au.LinkIndex], locAddr)
				}
			} else {
				utils.Infof("Netlink event: remove addr update %v", au.LinkAddress)
				// Address removal
				iIndex := au.LinkIndex
			lookLoop:
				for i, addrRaw := range nw.ifAddrs[iIndex] {
					addr, _ := net.ResolveUDPAddr("udp", addrRaw.String())
					if addr.IP.Equal(au.LinkAddress.IP) {
						nw.ifAddrs[iIndex] = append(nw.ifAddrs[iIndex][:i], nw.ifAddrs[iIndex][i+1:]...)
						nw.pconnMgr.closePconn(addr)
						break lookLoop
					}
				}
			}
		case lu := <-nw.linkUpdates:
			// We should only monitor link loss, if it comes back, we will receive address updates
			if lu.Flags&0x02 > 0 { // LINK_DOWN flag
				//utils.Infof("Netlink event: link %s down", lu.Attrs().Name)
				//iIndex := lu.Attrs().Index
				//for _, addr := range nw.ifAddrs[iIndex] {
				//	nw.pconnMgr.closePconn(addr)
				//}
				//nw.ifAddrs[iIndex] = nil
			}
		}
	}
	// Unsubscribe from netlink events
	close(nw.doneAU)
	// XXX Seems closing Link subscription breaks everything...
	// close(nw.doneLU)
	// Close pconns
	nw.pconnMgr.closePconns()
}

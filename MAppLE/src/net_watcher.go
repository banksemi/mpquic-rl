package quic

import "net"

type NetWatcherI interface {
	getInterfaceName(addr net.UDPAddr) (string, bool)
	lostPconn()
	setup()
	run()
}

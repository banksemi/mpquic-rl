package utils

import "net"

// GetIPVersion of the ip provided (either 4 or 6)
func GetIPVersion(ip net.IP) int {
	if ip.To4() != nil {
		return 4
	}
	return 6
}

package utils

import (
	"net"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("IP", func() {
	It("detects IPv4 address", func() {
		addr, _ := net.ResolveUDPAddr("udp", "2.89.57.75:1337")
		Expect(GetIPVersion(addr.IP)).To(Equal(4))
	})

	It("detects IPv6 address", func() {
		addr, _ := net.ResolveUDPAddr("udp", "[dead:beef:cafe:babe::c001:1337]:1337")
		Expect(GetIPVersion(addr.IP)).To(Equal(6))
	})
})

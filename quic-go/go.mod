module github.com/lucas-clemente/quic-go

go 1.14

require (
	bitbucket.com/marcmolla/gorl v0.0.0
	github.com/atgjack/prob v0.0.0-20161220081030-6cfd5d401186
	github.com/aunum/gold v0.0.0-20201022151355-225e849d893f // indirect
	github.com/bifurcation/mint v0.0.0-20210616192047-fd18df995463
	github.com/golang/mock v1.4.3
	github.com/hashicorp/golang-lru v0.5.4
	github.com/klauspost/cpuid v1.2.3 // indirect
	github.com/klauspost/reedsolomon v1.9.3
	github.com/lucas-clemente/aes12 v0.0.0-20171027163421-cd47fb39b79f
	github.com/lucas-clemente/fnv128a v0.0.0-20160504152609-393af48d3916
	github.com/lucas-clemente/quic-clients v0.1.0
	github.com/lucas-clemente/quic-go-certificates v0.0.0-20160823095156-d2f86524cced
	github.com/onsi/ginkgo v1.12.0
	github.com/onsi/gomega v1.9.0
	github.com/vishvananda/netlink v1.1.0
	golang.org/x/crypto v0.0.0-20220518034528-6f7dac969898
	golang.org/x/net v0.0.0-20211112202133-69e39bad7dc2
	golang.org/x/tools/gopls v0.6.4 // indirect
	gonum.org/v1/gonum v0.11.0 // indirect
	gonum.org/v1/hdf5 v0.0.0-20210714002203-8c5d23bc6946 // indirect
	honnef.co/go/tools v0.1.3
)

replace bitbucket.com/marcmolla/gorl => ../gorl
replace github.com/bifurcation/mint => ../mint-a6080d464fb57a9330c2124ffb62f3c233e3400e

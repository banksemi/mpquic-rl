module github.com/caddyserver/caddy

go 1.13

require (
	github.com/aunum/gold v0.0.0-20201022151355-225e849d893f // indirect
	github.com/aunum/log v0.0.0-20200821225356-38d2e2c8b489 // indirect
	github.com/bifurcation/mint v0.0.0-20210616192047-fd18df995463 // indirect
	github.com/dustin/go-humanize v1.0.0
	github.com/fatih/color v1.9.0 // indirect
	github.com/flynn/go-shlex v0.0.0-20150515145356-3f9db97f8568
	github.com/go-acme/lego/v3 v3.2.0
	github.com/google/uuid v1.1.1
	github.com/gorilla/websocket v1.4.0
	github.com/hashicorp/go-syslog v1.0.0
	github.com/jimstudt/http-authentication v0.0.0-20140401203705-3eca13d6893a
	github.com/klauspost/cpuid v1.2.3
	github.com/kylelemons/godebug v0.0.0-20170820004349-d65d576e9348 // indirect
	github.com/lucas-clemente/quic-go v0.13.1
	github.com/mholt/certmagic v0.8.3
	github.com/naoina/go-stringutil v0.1.0 // indirect
	github.com/naoina/toml v0.1.1
	github.com/ory/dockertest v3.3.5+incompatible // indirect
	github.com/russross/blackfriday v0.0.0-20170610170232-067529f716f4
	golang.org/x/net v0.0.0-20220425223048-2871e0cb64e4
	golang.org/x/sys v0.0.0-20220422013727-9388b58f7150 // indirect
	golang.org/x/term v0.0.0-20220411215600-e5f449aeb171 // indirect
	gonum.org/v1/gonum v0.11.0 // indirect
	gonum.org/v1/hdf5 v0.0.0-20210714002203-8c5d23bc6946 // indirect
	gonum.org/v1/plot v0.11.0 // indirect
	gopkg.in/mcuadros/go-syslog.v2 v2.2.1
	gopkg.in/natefinch/lumberjack.v2 v2.0.0
	gopkg.in/yaml.v2 v2.2.8
	gorgonia.org/gorgonia v0.9.9 // indirect
	gorgonia.org/tensor v0.9.4 // indirect
)

replace github.com/aunum/gold => ../../gold

replace bitbucket.com/marcmolla/gorl => ../gorl

replace github.com/bifurcation/mint => ../mint-a6080d464fb57a9330c2124ffb62f3c233e3400e

replace github.com/lucas-clemente/quic-go => ../peekaboo

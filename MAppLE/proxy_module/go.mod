module dash/client

go 1.12

require (
	github.com/aunum/gold v0.0.0-20201022151355-225e849d893f // indirect
	github.com/lucas-clemente/quic-go v0.0.0
	golang.org/x/crypto v0.0.0-20220427172511-eb4f295cb31f // indirect
	golang.org/x/net v0.0.0-20220425223048-2871e0cb64e4
	golang.org/x/sys v0.0.0-20220422013727-9388b58f7150 // indirect
	golang.org/x/term v0.0.0-20220411215600-e5f449aeb171 // indirect
)

replace github.com/lucas-clemente/quic-go => ../src

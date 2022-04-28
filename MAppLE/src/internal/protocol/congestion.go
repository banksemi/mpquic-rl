package protocol

type CongestionControlID byte

const (
	CongestionControlOlia      = 1
	CongestionControlCubic     = 2
	CongestionControlCubicReno = 3
	CongestionControlPrr       = 4
)

func ParseCongestionControl(cc string) CongestionControlID {
	switch cc {
	case "olia":
		return CongestionControlOlia
	case "cubic":
		return CongestionControlCubic
	case "cubic-reno":
		return CongestionControlCubicReno
	case "prr":
		return CongestionControlPrr
	}
	print(cc)
	panic(" unknown congestion control scheme")
}

package protocol

type SchedulingSchemeID byte

var SEDPF_WINDOW_SIZE = 5

const SIOD_MaxSchedulingWindow int = 8

const (
	SchedRR               = 1
	SchedLossRate         = 2
	SchedLowLatency       = 3
	SchedHighestRemaining = 4
	SchedReferenceS_EDPF  = 5
	SchedRTTS_EDPF        = 6
	SchedArrivalS_EDPF    = 7
	SchedSimpleS_EDPF     = 8
	SchedS_IOD            = 9
	SchedIOD              = 10
	SchedO_IOD            = 11
	SchedRL               = 12
	SchedSingle           = 100 // debugging
)

func ParseSchedulingScheme(scheme string) SchedulingSchemeID {
	switch scheme {
	case "rr":
		return SchedRR
	case "lr":
		return SchedLossRate
	case "ll":
		return SchedLowLatency
	case "hr":
		return SchedHighestRemaining
	case "s-edpf-w5":
		SEDPF_WINDOW_SIZE = 5
		return SchedReferenceS_EDPF
	case "s-edpf-w10":
		SEDPF_WINDOW_SIZE = 10
		return SchedReferenceS_EDPF
	case "s-edpf-w25":
		SEDPF_WINDOW_SIZE = 25
		return SchedReferenceS_EDPF
	case "s-edpf-w50":
		SEDPF_WINDOW_SIZE = 50
		return SchedReferenceS_EDPF
	case "s-edpf-w100":
		SEDPF_WINDOW_SIZE = 100
		return SchedReferenceS_EDPF
	case "s-edpf":
		return SchedReferenceS_EDPF
	case "simple-s-edpf":
		return SchedSimpleS_EDPF
	case "rtt-s-edpf":
		return SchedRTTS_EDPF
	case "arr-s-edpf":
		return SchedArrivalS_EDPF
	case "s-iod":
		return SchedS_IOD
	case "iod":
		return SchedIOD
	case "o-iod":
		return SchedO_IOD
	case "rl":
		return SchedRL
	case "single":
		return SchedSingle
	}

	return 0
}

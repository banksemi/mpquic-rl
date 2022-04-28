package utils

func Lerp(a, b, t float64) float64 {
	return a*(1.0-t) + b*t
}

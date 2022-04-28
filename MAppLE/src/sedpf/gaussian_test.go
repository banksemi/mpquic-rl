package sedpf

import (
	"math"
	"testing"
)

var series = []float64{.34, .52, .51, .21, .71, .84, .69, .22, .49, .78}

func expectEqual(a, b float64, t *testing.T) {
	if a != b {
		t.Errorf("%f != %f", a, b)
	}
}

func expectAlmostEqual(a, b, e float64, t *testing.T) {
	if math.Round(a*e)/e != math.Round(b*e)/e {
		t.Errorf("%f != %f", a, b)
	}
}

func TestFromSeries(t *testing.T) {
	g := NewGaussianFromSeries(series)
	expectEqual(g.Mean, 0.531, t)
	expectAlmostEqual(g.Variance, 0.045, 1000, t)
}

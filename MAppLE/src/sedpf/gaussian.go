package sedpf

import (
	"fmt"
	"math"
)

type Gaussian struct {
	Mean     float64
	Variance float64
}

func sum(elems []float64) float64 {
	var a float64 = 0
	for i := 0; i < len(elems); i++ {
		a += elems[i]
	}
	return a
}

func NewGaussian(Mean, Variance float64) Gaussian {
	return Gaussian{
		Mean:     Mean,
		Variance: Variance,
	}
}

// https://github.com/chobie/go-gaussian/blob/master/gaussian.go
func NewGaussianFromPrecision(precision, precisionMean float64) Gaussian {
	return NewGaussian(precisionMean/precision, 1.0/precision)
}

func NewGaussianFromSeries(series []float64) Gaussian {
	mean := sum(series) / float64(len(series))

	var variance float64
	for _, x := range series {
		variance += math.Pow(x-mean, 2)
	}
	variance /= float64(len(series))

	return NewGaussian(mean, variance)
}

// Probability density function (phi)
func Pdf(x float64) float64 {
	return math.Exp(-((x * x) / 2)) / math.Sqrt(2*math.Pi)
}

// Cumulative Distribution Function (Phi)
func Cdf(x float64) float64 {
	return math.Erfc(-(x / math.Sqrt2)) / 2
}

func CoVariance(u, v Gaussian) float64 {
	return u.Mul(v).Mean - u.Mean*v.Mean
}

func CorrelationCoefficient(u, v Gaussian) float64 {
	return CoVariance(u, v) / (u.StdDev() * v.StdDev())
}

func (u Gaussian) StdDev() float64 {
	return math.Sqrt(u.Variance)
}

func (u Gaussian) Add(v Gaussian) Gaussian {
	return NewGaussian(u.Mean+v.Mean, u.Variance+v.Variance)
}

func (u Gaussian) Addf(v float64) Gaussian {
	return NewGaussian(u.Mean+v, u.Variance)
}

func (u Gaussian) Subf(v float64) Gaussian {
	return NewGaussian(u.Mean-v, u.Variance)
}

func (u Gaussian) Sub(v Gaussian) Gaussian {
	return NewGaussian(u.Mean-v.Mean, u.Variance-v.Variance)
}

// https://github.com/chobie/go-gaussian/blob/master/gaussian.go
func (u Gaussian) Mul(v Gaussian) Gaussian {
	precision := 1.0 / u.Variance
	dprecision := 1.0 / v.Variance
	return NewGaussianFromPrecision(precision+dprecision, precision*u.Mean+dprecision*v.Mean)
}

// http://www1.up.poznan.pl/cb48/prezentacje/Oliveira.pdf
func Max(u, v Gaussian) Gaussian {
	// XXX assume uncorrelated distributions
	// this need not be correct

	mean := u.Mean * v.Mean

	variance := u.Mean * u.Mean * v.Variance
	variance += v.Mean * v.Mean * u.Variance
	variance += u.Variance * v.Variance

	return NewGaussian(mean, variance)
}

func MaxPartitionMBT(queue []Gaussian) Gaussian {
	g := queue[0]     // g is first element in queue
	queue = queue[1:] // pop g

	for len(queue) > 2 {
		queue = append(queue, g) // append g to the end of the queue
		i := queue[0]
		j := queue[1]
		queue = queue[2:] // pop i and j
		g = Max(i, j)
	}

	return g
}

func (g Gaussian) ToString() string {
	return fmt.Sprintf("Mean: %f, Variance: %f", g.Mean, g.Variance)
}

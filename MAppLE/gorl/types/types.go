package types

import "fmt"

// Shape of the tensors (2-D)
type Shape [2]int

// Output format
type Output float32

// Weights format
type Weights [][]Output

// Bias format
type Bias []Output

// Format for state
type Vector []Output

func (b Bias) IsEqual(other Bias) bool {
	if len(b) != len(other) {
		return false
	}
	for i := 0; i < len(b); i++ {
		if b[i] != other[i] {
			return false
		}
	}
	return true
}

func (b Bias) String() string {
	output := "["
	for key := range b {
		output += fmt.Sprintf("%.5f ", b[key])
	}
	return output + "]"
}

func (v Vector) IsEqual(other Vector) bool{
	if len(v) != len(other) {
		return false
	}
	for i := 0; i < len(v); i++ {
		if v[i] != other[i] {
			return false
		}
	}
	return true
}

func (v Vector) String() string {
	output := "["
	for key := range v {
		output += fmt.Sprintf("%.5f ", v[key])
	}
	return output + "]"
}

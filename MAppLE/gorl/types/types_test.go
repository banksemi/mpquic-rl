package types

import (
	"fmt"
	"testing"
)

func TestBias_IsEqual(t *testing.T) {
	a := Bias{1, 2, 3}
	if !a.IsEqual(Bias{1, 2, 3}) {
		t.Errorf("%s should be equal to %s", a, Bias{1, 2, 3})
	}
	if a.IsEqual(Bias{1, 2, 4}) || a.IsEqual(Bias{4, 5}) || a.IsEqual(Bias{1, 2, 3, 4}) {
		t.Errorf("%s should be not equal", a)
	}
}

func TestBias_String(t *testing.T) {
	a := Bias{1, 2, 3}
	if fmt.Sprintf("%s", a) != "[1.00000 2.00000 3.00000 ]" {
		t.Errorf("%s should be [1.00000 2.00000 3.00000 ]", a)
	}
}

func TestVector_IsEqual(t *testing.T) {
	a := Vector{1, 2, 3}
	if !a.IsEqual(Vector{1, 2, 3}) {
		t.Errorf("%s should be equal to %s", a, Vector{1, 2, 3})
	}
	if a.IsEqual(Vector{1, 2, 4}) || a.IsEqual(Vector{4, 5}) || a.IsEqual(Vector{1, 2, 3, 4}) {
		t.Errorf("%s should be not equal", a)
	}
}

func TestVector_String(t *testing.T) {
	a := Vector{1, 2, 3}
	if fmt.Sprintf("%s", a) != "[1.00000 2.00000 3.00000 ]" {
		t.Errorf("%s should be [1.00000 2.00000 3.00000 ]", a)
	}
}
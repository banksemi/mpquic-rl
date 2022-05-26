package activations

import (
	gorl "bitbucket.com/marcmolla/gorl/types"
	"math/rand"
	"testing"
)

func TestSigmoid(t *testing.T) {
	if Sigmoid(gorl.Output(0)) != 0.5 {
		t.Errorf("error in sigmoid activation function: expected sigmoid(0)=0.5, obtained %f",
			Sigmoid(gorl.Output(0)))
	}
	if 1-Sigmoid(gorl.Output(100)) > 0 {
		t.Errorf("error, sigmoid(100)= %f, expected 1.", Sigmoid(gorl.Output(100)))
	}
}

func TestLinear(t *testing.T) {
	random := gorl.Output(rand.Float32()*100 - 50)
	if Linear(random) != random {
		t.Errorf("Linear is not linear: expected %f, obtained %f", random, Linear(random))
	}
}

func TestRelu(t *testing.T) {
	for i := 0; i < 10; i++ {
		random := gorl.Output(rand.Float32()*100 - 50)
		if random > 0 && Relu(random) != random {
			t.Errorf("Relu is not linear for positive: expected %f, obtained %f", random, Relu(random))
		}
		if random <= 0 && Relu(random) != 0 {
			t.Errorf("Relu is not 0 for negative: expected %f, obtained %f", random, Relu(random))
		}
	}
}

func TestBinaryStep(t *testing.T) {
	for i := 0; i < 10; i++ {
		random := gorl.Output(rand.Float32()*100 - 50)
		if random > 0 && BinaryStep(random) != 1 {
			t.Errorf("BinaryStep for positive: expected %f, obtained %f", random, BinaryStep(random))
		}
		if random <= 0 && BinaryStep(random) != 0 {
			t.Errorf("BinarySteo for negative: expected %f, obtained %f", random, BinaryStep(random))
		}
	}
}

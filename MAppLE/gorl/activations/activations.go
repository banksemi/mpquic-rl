package activations

import (
	gorl "bitbucket.com/marcmolla/gorl/types"
	"math"
)

type ActivationFunction func(output gorl.Output) gorl.Output

func Linear(output gorl.Output) gorl.Output {
	return output
}

func Relu(output gorl.Output) gorl.Output {
	if output < 0 {
		return 0.0
	}
	return output
}

func Sigmoid(output gorl.Output) gorl.Output {
	return gorl.Output(1.0 / (1.0 + math.Exp(float64(-output))))
}

func BinaryStep(output gorl.Output) gorl.Output {
	if output < 0 {
		return 0
	}
	return 1
}

package main

import (
	"fmt"
	"time"

	"bitbucket.com/marcmolla/gorl/activations"
	"bitbucket.com/marcmolla/gorl/model"
	rl "bitbucket.com/marcmolla/gorl/types"
)

func main() {
	dense_1_bias := rl.Bias{-1.2858192, -0.9331074}

	dense_1_kernel := rl.Weights{
		{-0.03954726, 0.5797606, 0.01949516, 0.40404177, 0.018947445, -0.20699406, 0.57578695, -0.36765462, 0.5019975},
		{0.085874215, -0.036761623, 0.085415065, 0.2237164, 0.30428863, 0.38542396, 0.14576864, -0.013444869, 0.092598215},
	}

	input := rl.Vector{1, 1, 1, 1, 1, 1, 0, 0, 0}
	dense_2_kernel := rl.Weights{
		{-0.03954726, 0.5797606},
		{0.085874215, -0.036761623},
	}

	myModel := model.DNN{}
	hiddenLayer := model.Dense{Size: 2, ActFunction: activations.Relu}
	hiddenLayer.InitLayer(dense_1_kernel, dense_1_bias)
	myModel.AddLayer(&hiddenLayer)
	hiddenLayer2 := model.Dense{Size: 2, ActFunction: activations.Linear}
	hiddenLayer2.InitLayer(dense_2_kernel, dense_1_bias)
	myModel.AddLayer(&hiddenLayer2)

	fmt.Println(myModel.ModelSummary())

	start := time.Now()

	output := myModel.Compute(input)

	elapsed := time.Since(start)
	fmt.Println("Result: ", output)
	fmt.Printf("Took %s\n", elapsed)

}

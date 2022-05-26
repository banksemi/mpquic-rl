package agents

import (
	"bitbucket.com/marcmolla/gorl/activations"
	"bitbucket.com/marcmolla/gorl/model"
	gorl "bitbucket.com/marcmolla/gorl/types"
	"testing"
)

func createDQNAgent() DQNAgent {
	myPolicy := ArgMax{}

	myModel := model.DNN{}
	hiddenLayer1 := model.Dense{Size: 256, ActFunction: activations.Relu}
	hiddenLayer2 := model.Dense{Size: 256, ActFunction: activations.Relu}
	hiddenLayer3 := model.Dense{Size: 256, ActFunction: activations.Relu}
	hiddenLayer4 := model.Dense{Size: 9, ActFunction: activations.Linear}
	myModel.AddLayer(&hiddenLayer1)
	myModel.AddLayer(&hiddenLayer2)
	myModel.AddLayer(&hiddenLayer3)
	myModel.AddLayer(&hiddenLayer4)

	myAgent := DQNAgent{Policy: &myPolicy, QModel: &myModel}
	myAgent.LoadWeights("dqn_TicTacToe-Random-v0_weights.h5f")

	return myAgent
}
func BenchmarkBase(b *testing.B) {
	weight := gorl.Output(10.1)
	bias := gorl.Output(1.2)
	output := gorl.Output(0.0)
	input := gorl.Output(1.0)
	for i := 0; i < b.N; i++ {
		for layer := 0; layer < 4; layer++ {
			for neuronID := 0; neuronID < 256; neuronID++ {
				for j := 0; j < 256; j++ {
					output += input * weight
				}
			}
			output += bias
			output = activations.Relu(output)
		}
	}
}
func BenchmarkAgent(b *testing.B) {
	myAgent := createDQNAgent()
	state := gorl.Vector{1, 1, 0, 0, 0, -1, -1, 0, 0}
	// It seems that process time of above code does not impact in final results
	// b.ResetTimer()
	for i := 0; i < b.N; i++ {
		myAgent.GetAction(state)
	}
}
func BenchmarkModel(b *testing.B) {
	myAgent := createDQNAgent()
	state := gorl.Vector{1, 1, 0, 0, 0, -1, -1, 0, 0}
	for i := 0; i < b.N; i++ {
		myAgent.QModel.Compute(state)
	}
}
func BenchmarkPolicy(b *testing.B) {
	myAgent := createDQNAgent()
	state := gorl.Vector{1, 1, 0, 0, 0, -1, -1, 0, 0}
	qVector := myAgent.QModel.Compute(state)
	for i := 0; i < b.N; i++ {
		myAgent.Policy.Select(qVector)
	}
}
func BenchmarkNetwork(b *testing.B) {
	myAgent := createDQNAgent()
	state := gorl.Vector{1, 1, 0, 0, 0, -1, -1, 0, 0}
	layers := myAgent.QModel.GetLayers()
	output := state
	for i := 0; i < b.N; i++ {
		for _, layer := range layers {
			output = layer.Compute(output)
		}
	}
}
func BenchmarkLayer(b *testing.B) {
	myAgent := createDQNAgent()
	state := gorl.Vector{1, 1, 0, 0, 0, -1, -1, 0, 0}
	layers := myAgent.QModel.GetLayers()
	output := layers[0].Compute(state)
	for i := 0; i < b.N; i++ {
		output = layers[1].Compute(output)
	}
}
func BenchmarkRawLayer(b *testing.B) {
	myAgent := createDQNAgent()
	state := gorl.Vector{1, 1, 0, 0, 0, -1, -1, 0, 0}
	layers := myAgent.QModel.GetLayers()
	output := make([]gorl.Output, len(layers[1].GetWeights()))
	input := layers[0].Compute(state)
	for i := 0; i < b.N; i++ {
		for neuronID, neuron := range layers[1].GetWeights() {
			var outNet gorl.Output = 0
			for id, weight := range neuron {
				outNet += input[id] * weight
			}
			output[neuronID] = layers[1].GetActFunction()(outNet + layers[1].GetBias()[neuronID])
		}
	}
}

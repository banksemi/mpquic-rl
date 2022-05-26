package model

import (
	"bitbucket.com/marcmolla/gorl/activations"
	gorl "bitbucket.com/marcmolla/gorl/types"
	"fmt"
)

// Model defines a interface for building a network model for the agents
type Model interface {
	// Adds a layer to the model.
	// Layes are always append to the existing ones
	AddLayer(layer Layer)

	// Returns a string with the humad-readable summary of the model
	ModelSummary() string

	// Compute the model and generates a output Vector from the input state
	Compute(state gorl.Vector) gorl.Vector
	InitLayer(layerNumber int, weights gorl.Weights, bias gorl.Bias)
	GetLayers() []Layer
}

type DNN struct {
	layers []Layer
}

func (d *DNN) AddLayer(layer Layer) {
	var inputSize int
	if d.layers != nil {
		inputSize = d.layers[len(d.layers)-1].GetLayerSize()
	}
	layer.SetInputShape(inputSize)
	d.layers = append(d.layers, layer)
}

func (d DNN) ModelSummary() string {
	output := "Model Summary\n"
	output += "-------------\n"
	for _, layer := range d.layers {
		output += layer.Summary()
	}
	return output
}

func (d DNN) Compute(state gorl.Vector) gorl.Vector {
	output := state
	for _, layer := range d.layers {
		output = layer.Compute(output)
	}
	return output
}

func (d *DNN) InitLayer(layerNumber int, weights gorl.Weights, bias gorl.Bias) {
	d.layers[layerNumber].InitLayer(weights, bias)
}

func (d *DNN) GetLayers() []Layer {
	return d.layers
}

type Layer interface {
	InitLayer(weights gorl.Weights, bias gorl.Bias)
	Compute(input gorl.Vector) gorl.Vector
	Summary() string
	SetInputShape(inputShape int)
	GetLayerSize() int
	// Benchmarking
	GetWeights() gorl.Weights
	GetBias() gorl.Bias
	GetActFunction() activations.ActivationFunction
}

type Dense struct {
	Size        int
	weights     gorl.Weights
	bias        gorl.Bias
	shape       gorl.Shape
	inputShape  int
	ActFunction activations.ActivationFunction
	output      []gorl.Output
}

func (d *Dense) GetLayerSize() int {
	return d.Size
}
func (d *Dense) GetWeights() gorl.Weights {
	return d.weights
}
func (d *Dense) GetBias() gorl.Bias {
	return d.bias
}
func (d *Dense) GetActFunction() activations.ActivationFunction {
	return d.ActFunction
}
func (d *Dense) InitLayer(weights gorl.Weights, bias gorl.Bias) {
	d.weights = weights
	d.bias = bias
	d.shape = gorl.Shape{len(weights), len(weights[0])}
	d.output = make([]gorl.Output, len(d.weights))
}

func (d *Dense) SetInputShape(inputShape int) {
	d.inputShape = inputShape
}

func (d *Dense) Compute(input gorl.Vector) gorl.Vector {
	for neuronID, neuron := range d.weights {
		var outNet gorl.Output = 0
		for id, weight := range neuron {
			outNet += input[id] * weight
		}
		d.output[neuronID] = d.ActFunction(outNet + d.bias[neuronID])
	}
	return d.output
}

func (d *Dense) Summary() string {
	return fmt.Sprintf("Dense, size: %d \n", d.Size)
}

package gorl

import (
	"bitbucket.com/marcmolla/gorl/activations"
	"bitbucket.com/marcmolla/gorl/agents"
	model "bitbucket.com/marcmolla/gorl/model"
	"encoding/json"
	"sync"
)

type layerSpec struct {
	LayerType       string `json:"type"`
	LayerSize       int    `json:"size"`
	LayerActivation string `json:"activation"`
}

type networkSpec struct {
	Layers []layerSpec `json:"layers"`
}

func NewDQNAgent(modelSpec string) agents.Agent {
	networkSpec := decodeSpec(modelSpec)
	policy := agents.ArgMax{}
	rModel := model.DNN{}
	for i := 0; i < len(networkSpec.Layers)-1; i++ {
		rModel.AddLayer(&model.Dense{Size: networkSpec.Layers[i].LayerSize,
			ActFunction: decodeActivation(networkSpec.Layers[i].LayerActivation)})
	}
	rModel.AddLayer(&model.Dense{Size: networkSpec.Layers[len(networkSpec.Layers)-1].LayerSize,
		ActFunction: decodeActivation(networkSpec.Layers[len(networkSpec.Layers)-1].LayerActivation)})
	rAgent := agents.DQNAgent{Policy: &policy, QModel: &rModel}
	return &rAgent
}

func NewTrainingDQNAgent(modelSpec string, path string, epsilon float32) agents.TrainingAgent {
	networkSpec := decodeSpec(modelSpec)
	policy := agents.E_greedy{Epsilon:epsilon}
	rModel := model.DNN{}
	for i := 0; i < len(networkSpec.Layers)-1; i++ {
		rModel.AddLayer(&model.Dense{Size: networkSpec.Layers[i].LayerSize,
			ActFunction: decodeActivation(networkSpec.Layers[i].LayerActivation)})
	}
	rModel.AddLayer(&model.Dense{Size: networkSpec.Layers[len(networkSpec.Layers)-1].LayerSize,
		ActFunction: decodeActivation(networkSpec.Layers[len(networkSpec.Layers)-1].LayerActivation)})
	rAgent := agents.TrainingDQNAgent{Path: path, DQNAgent: agents.DQNAgent{Policy: &policy, QModel: &rModel}}
	return &rAgent
}

func decodeSpec(spec string) *networkSpec {
	netSpec := &networkSpec{Layers: []layerSpec{}}
	err := json.Unmarshal([]byte(spec), netSpec)
	if err != nil {
		panic(err)
	}
	return netSpec
}

func decodeActivation(activation string) activations.ActivationFunction {
	switch activation {
	case "linear":
		return activations.Linear
	case "relu":
		return activations.Relu
	case "sigmoid":
		return activations.Sigmoid
	case "bynary_step":
		return activations.BinaryStep
	default:
		panic(activation)

	}
}

var instance 			agents.Agent
var instanceTraining 	agents.TrainingAgent
var once				sync.Once
var onceTraining		sync.Once

// Implements singleton pattern
func GetNormalInstance(modelSpec string) agents.Agent{
	once.Do(func(){
		instance = NewDQNAgent(modelSpec)
	})
	return instance
}

// Implements singleton pattern
func GetTrainingInstance(modelSpec string, path string, epsilon float32) agents.TrainingAgent{
	onceTraining.Do(func(){
		instanceTraining = NewTrainingDQNAgent(modelSpec, path, epsilon)
	})
	return instanceTraining
}
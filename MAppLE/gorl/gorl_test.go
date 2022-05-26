package gorl

import (
	"bitbucket.com/marcmolla/gorl/types"
	"testing"
)

func TestNewDQNAgent(t *testing.T) {
	modelSpec := `{"layers":[{"type":"Dense", "size":256, "activation":"relu"},
{"type":"dense", "size":256, "activation":"relu"},
{"type":"dense", "size":256, "activation":"relu"},
{"type":"dense", "size":9, "activation":"linear"}]}`

	myAgent := NewDQNAgent(modelSpec)
	myAgent.LoadWeights("./agents/dqn_TicTacToe-Random-v0_weights.h5f")

	state := types.Vector{0, 0, 0, 0, 0, 0, 0, 0, 0}
	generateError(t, 6, myAgent.GetAction(state), state)
	state = types.Vector{1, 0, 0, 0, 0, 0, 0, 0, 0}
	generateError(t, 4, myAgent.GetAction(state), state)
	state = types.Vector{1, 0, 0, 0, 0, -1, 0, 0, 0}
	generateError(t, 3, myAgent.GetAction(state), state)
	state = types.Vector{1, 1, 0, 0, 0, -1, 0, 0, 0}
	generateError(t, 2, myAgent.GetAction(state), state)
	state = types.Vector{1, 1, 0, 0, 0, -1, -1, 0, 0}
	generateError(t, 4, myAgent.GetAction(state), state)
}

func TestNewTrainingDQNAgent(t *testing.T) {
	modelSpec := `{"layers":[{"type":"Dense", "size":256, "activation":"relu"},
{"type":"dense", "size":256, "activation":"relu"},
{"type":"dense", "size":256, "activation":"relu"},
{"type":"dense", "size":9, "activation":"linear"}]}`
	myAgent := NewTrainingDQNAgent(modelSpec, "./", 0.)
	myAgent.LoadWeights("./agents/dqn_TicTacToe-Random-v0_weights.h5f")

	state := types.Vector{0, 0, 0, 0, 0, 0, 0, 0, 0}
	generateError(t, 6, myAgent.GetAction(state), state)
	state = types.Vector{1, 0, 0, 0, 0, 0, 0, 0, 0}
	generateError(t, 4, myAgent.GetAction(state), state)
	state = types.Vector{1, 0, 0, 0, 0, -1, 0, 0, 0}
	generateError(t, 3, myAgent.GetAction(state), state)
	state = types.Vector{1, 1, 0, 0, 0, -1, 0, 0, 0}
	generateError(t, 2, myAgent.GetAction(state), state)
	state = types.Vector{1, 1, 0, 0, 0, -1, -1, 0, 0}
	generateError(t, 4, myAgent.GetAction(state), state)
}

func TestGetNormalInstance(t *testing.T) {
	modelSpec := `{"layers":[{"type":"Dense", "size":256, "activation":"relu"},
{"type":"dense", "size":256, "activation":"relu"},
{"type":"dense", "size":256, "activation":"relu"},
{"type":"dense", "size":9, "activation":"linear"}]}`

	myAgent := GetNormalInstance(modelSpec)
	myAgent.LoadWeights("./agents/dqn_TicTacToe-Random-v0_weights.h5f")

	state := types.Vector{0, 0, 0, 0, 0, 0, 0, 0, 0}
	generateError(t, 6, myAgent.GetAction(state), state)
	state = types.Vector{1, 0, 0, 0, 0, 0, 0, 0, 0}
	generateError(t, 4, myAgent.GetAction(state), state)
	state = types.Vector{1, 0, 0, 0, 0, -1, 0, 0, 0}
	generateError(t, 3, myAgent.GetAction(state), state)
	state = types.Vector{1, 1, 0, 0, 0, -1, 0, 0, 0}
	generateError(t, 2, myAgent.GetAction(state), state)
	state = types.Vector{1, 1, 0, 0, 0, -1, -1, 0, 0}
	generateError(t, 4, myAgent.GetAction(state), state)

	myAgent = GetNormalInstance(modelSpec)
	state = types.Vector{0, 0, 0, 0, 0, 0, 0, 0, 0}
	generateError(t, 6, myAgent.GetAction(state), state)
	state = types.Vector{1, 0, 0, 0, 0, 0, 0, 0, 0}
	generateError(t, 4, myAgent.GetAction(state), state)
	state = types.Vector{1, 0, 0, 0, 0, -1, 0, 0, 0}
	generateError(t, 3, myAgent.GetAction(state), state)
	state = types.Vector{1, 1, 0, 0, 0, -1, 0, 0, 0}
	generateError(t, 2, myAgent.GetAction(state), state)
	state = types.Vector{1, 1, 0, 0, 0, -1, -1, 0, 0}
	generateError(t, 4, myAgent.GetAction(state), state)
}

func TestGetTrainingInstance(t *testing.T) {
	modelSpec := `{"layers":[{"type":"Dense", "size":256, "activation":"relu"},
{"type":"dense", "size":256, "activation":"relu"},
{"type":"dense", "size":256, "activation":"relu"},
{"type":"dense", "size":9, "activation":"linear"}]}`
	myAgent := GetTrainingInstance(modelSpec, "./", 0.)
	myAgent.LoadWeights("./agents/dqn_TicTacToe-Random-v0_weights.h5f")

	state := types.Vector{0, 0, 0, 0, 0, 0, 0, 0, 0}
	generateError(t, 6, myAgent.GetAction(state), state)
	state = types.Vector{1, 0, 0, 0, 0, 0, 0, 0, 0}
	generateError(t, 4, myAgent.GetAction(state), state)
	state = types.Vector{1, 0, 0, 0, 0, -1, 0, 0, 0}
	generateError(t, 3, myAgent.GetAction(state), state)
	state = types.Vector{1, 1, 0, 0, 0, -1, 0, 0, 0}
	generateError(t, 2, myAgent.GetAction(state), state)
	state = types.Vector{1, 1, 0, 0, 0, -1, -1, 0, 0}
	generateError(t, 4, myAgent.GetAction(state), state)

	myAgent = GetTrainingInstance(modelSpec, "./", 0.)

	state = types.Vector{0, 0, 0, 0, 0, 0, 0, 0, 0}
	generateError(t, 6, myAgent.GetAction(state), state)
	state = types.Vector{1, 0, 0, 0, 0, 0, 0, 0, 0}
	generateError(t, 4, myAgent.GetAction(state), state)
	state = types.Vector{1, 0, 0, 0, 0, -1, 0, 0, 0}
	generateError(t, 3, myAgent.GetAction(state), state)
	state = types.Vector{1, 1, 0, 0, 0, -1, 0, 0, 0}
	generateError(t, 2, myAgent.GetAction(state), state)
	state = types.Vector{1, 1, 0, 0, 0, -1, -1, 0, 0}
	generateError(t, 4, myAgent.GetAction(state), state)
}

func generateError(t *testing.T, expected int, obtained int, state types.Vector) {
	if expected != obtained {
		t.Errorf("State %v: expected %d, obtained %d", state, expected, obtained)
	}
}

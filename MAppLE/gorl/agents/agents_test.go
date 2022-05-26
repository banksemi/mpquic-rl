package agents

import (
	"bitbucket.com/marcmolla/gorl/activations"
	"bitbucket.com/marcmolla/gorl/model"
	gorl "bitbucket.com/marcmolla/gorl/types"
	"math"
	"reflect"
	"testing"
)

func TestLoadAgent(t *testing.T) {
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

	state := gorl.Vector{0, 0, 0, 0, 0, 0, 0, 0, 0}
	generateError(t, 6, myAgent.GetAction(state), state)
	state = gorl.Vector{1, 0, 0, 0, 0, 0, 0, 0, 0}
	generateError(t, 4, myAgent.GetAction(state), state)
	state = gorl.Vector{1, 0, 0, 0, 0, -1, 0, 0, 0}
	generateError(t, 3, myAgent.GetAction(state), state)
	state = gorl.Vector{1, 1, 0, 0, 0, -1, 0, 0, 0}
	generateError(t, 2, myAgent.GetAction(state), state)
	state = gorl.Vector{1, 1, 0, 0, 0, -1, -1, 0, 0}
	generateError(t, 4, myAgent.GetAction(state), state)
}

func generateError(t *testing.T, expected int, obtained int, state gorl.Vector) {
	if expected != obtained {
		t.Errorf("State %v: expected %d, obtained %d", state, expected, obtained)
	}
}

func TestE_greedy_Select(t *testing.T) {
	myPolicy := E_greedy{Epsilon: 0.}

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

	state := gorl.Vector{0, 0, 0, 0, 0, 0, 0, 0, 0}
	generateError(t, 6, myAgent.GetAction(state), state)
	state = gorl.Vector{1, 0, 0, 0, 0, 0, 0, 0, 0}
	generateError(t, 4, myAgent.GetAction(state), state)
	state = gorl.Vector{1, 0, 0, 0, 0, -1, 0, 0, 0}
	generateError(t, 3, myAgent.GetAction(state), state)
	state = gorl.Vector{1, 1, 0, 0, 0, -1, 0, 0, 0}
	generateError(t, 2, myAgent.GetAction(state), state)
	state = gorl.Vector{1, 1, 0, 0, 0, -1, -1, 0, 0}
	generateError(t, 4, myAgent.GetAction(state), state)
}

func TestE_greedy_Select_Random(t *testing.T) {
	myPolicy := E_greedy{Epsilon: 1.}

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
	state := gorl.Vector{0, 0, 0, 0, 0, 0, 0, 0, 0}
	average := 0.
	for i := 0; i < 100; i++ {
		average += float64(myAgent.GetAction(state)) / 100
	}
	if math.Abs(average-4) > 0.2 {
		t.Errorf("Expected avg(action) , obtained %f", average)
	}
}

func TestTrainingDQNAgent(t *testing.T) {
	myPolicy := E_greedy{Epsilon: 0.}

	myModel := model.DNN{}
	hiddenLayer1 := model.Dense{Size: 256, ActFunction: activations.Relu}
	hiddenLayer2 := model.Dense{Size: 256, ActFunction: activations.Relu}
	hiddenLayer3 := model.Dense{Size: 256, ActFunction: activations.Relu}
	hiddenLayer4 := model.Dense{Size: 9, ActFunction: activations.Linear}
	myModel.AddLayer(&hiddenLayer1)
	myModel.AddLayer(&hiddenLayer2)
	myModel.AddLayer(&hiddenLayer3)
	myModel.AddLayer(&hiddenLayer4)

	myAgent := TrainingDQNAgent{DQNAgent: DQNAgent{Policy: &myPolicy, QModel: &myModel}}
	myAgent.LoadWeights("dqn_TicTacToe-Random-v0_weights.h5f")

	state := gorl.Vector{0, 0, 0, 0, 0, 0, 0, 0, 0}
	generateError(t, 6, myAgent.GetAction(state), state)
	state = gorl.Vector{1, 0, 0, 0, 0, 0, 0, 0, 0}
	generateError(t, 4, myAgent.GetAction(state), state)
	state = gorl.Vector{1, 0, 0, 0, 0, -1, 0, 0, 0}
	generateError(t, 3, myAgent.GetAction(state), state)
	state = gorl.Vector{1, 1, 0, 0, 0, -1, 0, 0, 0}
	generateError(t, 2, myAgent.GetAction(state), state)
	state = gorl.Vector{1, 1, 0, 0, 0, -1, -1, 0, 0}
	generateError(t, 4, myAgent.GetAction(state), state)
}

func TestTrainingDQNAgent_SaveStep(t *testing.T) {

	myPolicy := E_greedy{Epsilon: 0.}
	myModel := model.DNN{}
	myAgent := TrainingDQNAgent{DQNAgent: DQNAgent{Policy: &myPolicy, QModel: &myModel}, Path: "./data"}
	myAgent.SaveStep(1, 0, gorl.Vector{1, 2, 3}, 0)
	myAgent.SaveStep(1, 1, gorl.Vector{3, 2, 1}, 1)
	myAgent.CloseEpisode(1, 10, false)
	episode, err := myAgent.GetEpisode(1)

	if err != nil {
		t.Error("error retrieving episode 1")
	}
	if !reflect.DeepEqual(episode[0], []string{"START", "[1.00000 2.00000 3.00000 ]", "0"}) {
		t.Errorf("episode %s not equal to [[START [1.00000 2.00000 3.00000 ] 0]]", episode[0])
	}
	if !reflect.DeepEqual(episode[1], []string{"1", "[3.00000 2.00000 1.00000 ]", "1"}) {
		t.Errorf("episode %s not equal to [[1 [3.00000 2.00000 1.00000 ] 1]]", episode[1])
	}
	if !reflect.DeepEqual(episode[2], []string{"10", "[3.00000 2.00000 1.00000 ]", "END"}) {
		t.Errorf("episode %s not equal to [[10 [3.00000 2.00000 1.00000 ] END]]", episode[2])
	}
}

func TestTrainingDQNAgent_GetEpisode(t *testing.T) {
	myPolicy := E_greedy{Epsilon: 0.}
	myModel := model.DNN{}
	myAgent := TrainingDQNAgent{DQNAgent: DQNAgent{Policy: &myPolicy, QModel: &myModel}}
	_, err := myAgent.GetEpisode(42)
	if err == nil {
		t.Error("expected error retrieving unexisting episode")
	}
}

func TestTrainingDQNAgent_partial(t *testing.T) {

	myPolicy := E_greedy{Epsilon: 0.}
	myModel := model.DNN{}
	myAgent := TrainingDQNAgent{DQNAgent: DQNAgent{Policy: &myPolicy, QModel: &myModel}, Path: "./data"}
	myAgent.SaveStep(1, 0, gorl.Vector{1, 2, 3}, 0)
	myAgent.SaveStep(1, 1, gorl.Vector{3, 2, 1}, 1)
	episode, err := myAgent.GetEpisode(1)
	myAgent.CloseEpisode(1, 10, true)

	if err != nil {
		t.Error("error retrieving episode 1")
	}
	if !reflect.DeepEqual(episode[0], []string{"START", "[1.00000 2.00000 3.00000 ]", "0"}) {
		t.Errorf("episode %s not equal to [[START [1.00000 2.00000 3.00000 ] 0]]", episode[0])
	}
	if !reflect.DeepEqual(episode[1], []string{"1", "[3.00000 2.00000 1.00000 ]", "1"}) {
		t.Errorf("episode %s not equal to [[1 [3.00000 2.00000 1.00000 ] 1]]", episode[1])
	}

	myAgent.SaveStep(1, 0, gorl.Vector{1, 2, 3}, 0)
	myAgent.SaveStep(1, 10, gorl.Vector{3, 2, 1}, 1)
	myAgent.CloseEpisode(1, 20, false)

	episode, err = myAgent.GetEpisode(1)

	if err != nil {
		t.Error("error retrieving episode 1")
	}
	if !reflect.DeepEqual(episode[0], []string{"START", "[1.00000 2.00000 3.00000 ]", "0"}) {
		t.Errorf("episode %s not equal to [[START [1.00000 2.00000 3.00000 ] 0]]", episode[0])
	}
	if !reflect.DeepEqual(episode[1], []string{"10", "[3.00000 2.00000 1.00000 ]", "1"}) {
		t.Errorf("episode %s not equal to [[10 [3.00000 2.00000 1.00000 ] 1]]", episode[1])
	}
	if !reflect.DeepEqual(episode[2], []string{"20", "[3.00000 2.00000 1.00000 ]", "END"}) {
		t.Errorf("episode %s not equal to [[20 [3.00000 2.00000 1.00000 ] END]]", episode[2])
	}
}

package agents

import (
	"bitbucket.com/marcmolla/gorl/model"
	gorl "bitbucket.com/marcmolla/gorl/types"
	"fmt"
	"math/rand"
	"time"
)

// PolicySelector defines the interface for selecting an action from a discrete action space.
type PolicySelector interface {
	// Selects the action from action vector.
	Select(actions gorl.Vector) int
}

type ArgMax struct{}

// Selects the action with the maximum Q-value
func (s *ArgMax) Select(actions gorl.Vector) int {
	action := -1
	var qMax gorl.Output
	for i, q := range actions {
		if action == -1 || q > qMax {
			action = i
			qMax = q
		}
	}
	return action
}

type E_greedy struct {
	Epsilon float32
	ArgMax
}

func (s *E_greedy) Select(actions gorl.Vector) int {
	if rand.Float32() < (s.Epsilon) {
		return rand.Intn(len(actions))
	}
	return s.ArgMax.Select(actions)
}

type Agent interface {
	LoadWeights(hdf5FileName string) error
	GetAction(state gorl.Vector) int
}

type DQNAgent struct {
	Policy PolicySelector
	QModel model.Model
}

func (d *DQNAgent) GetAction(state gorl.Vector) int {
	qVector := d.QModel.Compute(state)
	return d.Policy.Select(qVector)
}

func (d *DQNAgent) LoadWeights(hdf5FileName string) error {
	loadedData := LoadWeights(hdf5FileName)
	for layer := 0; layer < len(loadedData.Weights); layer++ {
		d.QModel.InitLayer(layer, loadedData.Weights[layer], loadedData.Bias[layer])
	}

	//Init seed
	rand.Seed(time.Now().UTC().UnixNano())
	
	return nil
}

type TrainingAgent interface {
	Agent
	CloseEpisode(id uint64, finalReward gorl.Output, partial bool)
	SaveStep(episodeID uint64, previousReward gorl.Output, state gorl.Vector, action int)
}

type TrainingDQNAgent struct {
	DQNAgent
	episodeSteps map[uint64][][]string
	episodeClosed	map[uint64]bool
	Path         string
}

func (t *TrainingDQNAgent) SaveStep(episodeID uint64, previousReward gorl.Output, state gorl.Vector, action int) {
	// Init episodes memory
	if t.episodeSteps == nil {
		t.episodeSteps = make(map[uint64][][]string)
		t.episodeClosed = make(map[uint64]bool)
	}
	steps, ok := t.episodeSteps[episodeID]
	var rewardStr string
	if !ok {
		steps = [][]string{}
		rewardStr = "START"
	} else {
		rewardStr = fmt.Sprint(previousReward)
	}
	steps = append(steps, []string{rewardStr, fmt.Sprint(state), fmt.Sprint(action)})
	t.episodeSteps[episodeID] = steps
}

func (t *TrainingDQNAgent) CloseEpisode(episodeID uint64, finalReward gorl.Output, partial bool) {
	// Only writeEpisode once
	if _, ok := t.episodeClosed[episodeID]; ok{
		return
	}

	if steps, ok := t.episodeSteps[episodeID]; ok {
		lastState := steps[len(steps)-1][1]
		lastStep := []string{fmt.Sprint(finalReward), lastState, "END"}
		t.episodeSteps[episodeID] = append(steps, lastStep)
		writeEpisode(t.episodeSteps[episodeID], episodeID, t.Path)
		// Only writeEpisode once if not partial
		if !partial{
			t.episodeClosed[episodeID] = true
		}else{
			t.episodeSteps = make(map[uint64][][]string)
		}
	}
}

func (t *TrainingDQNAgent) GetEpisode(episodeID uint64) ([][]string, error) {
	if episode, ok := t.episodeSteps[episodeID]; !ok {
		return nil, fmt.Errorf("episode not found: %d", episodeID)
	} else {
		return episode, nil
	}
}

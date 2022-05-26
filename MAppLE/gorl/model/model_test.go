package model

import (
	"bitbucket.com/marcmolla/gorl/activations"
	gorl "bitbucket.com/marcmolla/gorl/types"
	"testing"
)

func TestNot(t *testing.T) {
	myModel := DNN{}
	hiddenLayer := Dense{Size: 1, ActFunction: activations.Linear}
	hiddenLayer.InitLayer(gorl.Weights{{-1.0}}, gorl.Bias{1.0})
	myModel.AddLayer(&hiddenLayer)

	myModel.ModelSummary()

	if myModel.Compute(gorl.Vector{1})[0] != 0 {
		t.Error("Expected [0], got ", myModel.Compute(gorl.Vector{1}))
	}
	if myModel.Compute(gorl.Vector{0})[0] != 1 {
		t.Error("Expected [1], got ", myModel.Compute(gorl.Vector{1}))
	}
}

func TestOr(t *testing.T) {
	myModel := DNN{}
	hiddenLayer := Dense{Size: 1, ActFunction: activations.BinaryStep}
	hiddenLayer.InitLayer(gorl.Weights{{1, 1}}, gorl.Bias{-0.1})
	myModel.AddLayer(&hiddenLayer)

	myModel.ModelSummary()

	if myModel.Compute(gorl.Vector{0, 0})[0] != 0 {
		t.Error("Expected [0], got ", myModel.Compute(gorl.Vector{0, 0}))
	}
	if myModel.Compute(gorl.Vector{0, 1})[0] != 1 {
		t.Error("Expected [1], got ", myModel.Compute(gorl.Vector{0, 1}))
	}
	if myModel.Compute(gorl.Vector{1, 0})[0] != 1 {
		t.Error("Expected [1], got ", myModel.Compute(gorl.Vector{1, 0}))
	}
	if myModel.Compute(gorl.Vector{1, 1})[0] != 1 {
		t.Error("Expected [1], got ", myModel.Compute(gorl.Vector{1, 1}))
	}
}

func TestAnd(t *testing.T) {
	myModel := DNN{}
	hiddenLayer := Dense{Size: 1, ActFunction: activations.BinaryStep}
	hiddenLayer.InitLayer(gorl.Weights{{1, 1}}, gorl.Bias{-1.1})
	myModel.AddLayer(&hiddenLayer)

	myModel.ModelSummary()

	if myModel.Compute(gorl.Vector{0, 0})[0] != 0 {
		t.Error("Expected [0], got ", myModel.Compute(gorl.Vector{0, 0}))
	}
	if myModel.Compute(gorl.Vector{0, 1})[0] != 0 {
		t.Error("Expected [0], got ", myModel.Compute(gorl.Vector{0, 1}))
	}
	if myModel.Compute(gorl.Vector{1, 0})[0] != 0 {
		t.Error("Expected [0], got ", myModel.Compute(gorl.Vector{1, 0}))
	}
	if myModel.Compute(gorl.Vector{1, 1})[0] != 1 {
		t.Error("Expected [1], got ", myModel.Compute(gorl.Vector{1, 1}))
	}
}

func TestXOR(t *testing.T) {
	myModel := DNN{}
	hiddenLayer := Dense{Size: 2, ActFunction: activations.Relu}
	hiddenLayer.InitLayer(gorl.Weights{{1, 1}, {1, 1}}, gorl.Bias{0, -1})
	outputLayer := Dense{Size: 1, ActFunction: activations.Linear}
	outputLayer.InitLayer(gorl.Weights{{1, -2}}, gorl.Bias{0})
	myModel.AddLayer(&hiddenLayer)
	myModel.AddLayer(&outputLayer)

	myModel.ModelSummary()

	if myModel.Compute(gorl.Vector{0, 0})[0] != 0 {
		t.Error("Expected [0], got ", myModel.Compute(gorl.Vector{0, 0}))
	}
	if myModel.Compute(gorl.Vector{0, 1})[0] != 1 {
		t.Error("Expected [1], got ", myModel.Compute(gorl.Vector{0, 1}))
	}
	if myModel.Compute(gorl.Vector{1, 0})[0] != 1 {
		t.Error("Expected [1], got ", myModel.Compute(gorl.Vector{1, 0}))
	}
	if myModel.Compute(gorl.Vector{1, 1})[0] != 0 {
		t.Error("Expected [0], got ", myModel.Compute(gorl.Vector{1, 1}))
	}

}

func TestDNN_Leftovers(t *testing.T) {
	myModel := DNN{}
	hiddenLayer := Dense{Size: 2, ActFunction: activations.Relu}
	outputLayer := Dense{Size: 1, ActFunction: activations.Linear}

	myModel.AddLayer(&hiddenLayer)
	myModel.AddLayer(&outputLayer)

	myModel.ModelSummary()

	myModel.InitLayer(0, gorl.Weights{{1, 1}, {1, 1}}, gorl.Bias{0, -1})
	myModel.InitLayer(1, gorl.Weights{{1, -2}}, gorl.Bias{0})

	if myModel.Compute(gorl.Vector{0, 0})[0] != 0 {
		t.Error("Expected [0], got ", myModel.Compute(gorl.Vector{0, 0}))
	}
	if myModel.Compute(gorl.Vector{0, 1})[0] != 1 {
		t.Error("Expected [1], got ", myModel.Compute(gorl.Vector{0, 1}))
	}
	if myModel.Compute(gorl.Vector{1, 0})[0] != 1 {
		t.Error("Expected [1], got ", myModel.Compute(gorl.Vector{1, 0}))
	}
	if myModel.Compute(gorl.Vector{1, 1})[0] != 0 {
		t.Error("Expected [0], got ", myModel.Compute(gorl.Vector{1, 1}))
	}

	if !myModel.GetLayers()[0].GetBias().IsEqual(gorl.Bias{0, -1}) {
		t.Errorf("Expected %s, got %s", myModel.GetLayers()[0].GetBias(), gorl.Bias{0, -1})
	}
}

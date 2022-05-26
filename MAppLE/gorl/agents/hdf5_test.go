package agents

import (
	"gonum.org/v1/hdf5"
	"testing"
)

func TestHDF5(t *testing.T) {
	t.Log("=== go-hdf5 ===")
	version, err := hdf5.LibVersion()
	if err != nil {
		t.Errorf("** error ** %s", err)
	}
	t.Logf("=== version: %s", version)
	t.Logf("=== bye.")
}

func TestBasicRead(t *testing.T) {
	filename := "dqn_TicTacToe-Random-v0_weights.h5f"
	file, err := hdf5.OpenFile(filename, hdf5.F_ACC_RDONLY)
	if err != nil {
		t.Fatalf("File %s: %s", filename, err)
	}
	defer file.Close()
}

func TestHDF5Lib(t *testing.T) {
	agent := LoadWeights("dqn_TicTacToe-Random-v0_weights.h5f")
	if len(agent.Weights) != 4 || len(agent.Bias) != 4 {
		t.Errorf("Incorrect bias/weights dimension: %d, %d", len(agent.Weights), len(agent.Bias))
	}
	bias_test := agent.Bias[0][10]
	if bias_test != 0.34929872 {
		t.Errorf("Expexted bias[0][10] %f, got %f", 0.34929872, bias_test)
	}
	kernel_test := agent.Weights[3][5][237]
	if kernel_test != 0.10527793 {
		t.Errorf("Expexted Weights[4][5][237] %f, got %f", 0.10527793, kernel_test)
	}
}

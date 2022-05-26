package agents

import (
	gorl "bitbucket.com/marcmolla/gorl/types"
	"gonum.org/v1/hdf5"
	"strings"
)

type SavedAgent struct {
	Weights []gorl.Weights
	Bias    []gorl.Bias
}

func LoadWeights(filename string) SavedAgent {
	loadedAgent := SavedAgent{}
	file, err := hdf5.OpenFile(filename, hdf5.F_ACC_RDONLY)
	if err != nil {
		panic(err)
	}
	defer file.Close()
	if number, err := file.NumObjects(); err == nil {
		var i uint
		for i = 0; i < number; i++ {
			name, err := file.ObjectNameByIndex(i)
			if err != nil {
				panic(err)
			}
			if isDense(name) {
				bias, weights := handleGroup(name, file)
				loadedAgent.Weights = append(loadedAgent.Weights, weights)
				loadedAgent.Bias = append(loadedAgent.Bias, bias)
			}
		}
	}
	return loadedAgent
}

func handleGroup(name string, file *hdf5.File) (gorl.Bias, gorl.Weights) {
	group, err := file.OpenGroup(name)
	if err != nil {
		panic(err)
	}

	group, err = group.OpenGroup(name)
	if err != nil {
		panic(err)
	}

	// Load the Bias vector
	biasDataset, err := group.OpenDataset("bias:0")
	rBias := make(gorl.Bias, biasDataset.Space().SimpleExtentNPoints())
	err = biasDataset.Read(&rBias)
	if err != nil {
		panic(err)
	}

	// Load the weights
	weightsDataset, err := group.OpenDataset("kernel:0")
	dims, _, err := weightsDataset.Space().SimpleExtentDims()
	if err != nil {
		panic(err)
	}
	rawWeights := make([]gorl.Output, weightsDataset.Space().SimpleExtentNPoints())
	err = weightsDataset.Read(&rawWeights)
	// We need to transpose the matrix
	dim1 := int(dims[1])
	dim0 := int(dims[0])
	rWeights := make(gorl.Weights, dim1)
	for i := 0; i < dim1; i++ {
		rWeights[i] = make([]gorl.Output, dim0)
		for j := 0; j < dim0; j++ {
			rWeights[i][j] = rawWeights[j*dim1+i]
		}
	}
	if err != nil {
		panic(err)
	}
	return rBias, rWeights
}

func isDense(name string) bool {
	if layerType := strings.Split(name, "_"); layerType[0] == "dense" {
		return true
	}
	return false
}

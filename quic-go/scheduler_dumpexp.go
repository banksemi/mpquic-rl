package quic

import (
	"fmt"
	"os"
	"encoding/csv"
)

type experienceAgent struct{
	experiences		map[uint64][][]string
}

func (eag *experienceAgent) Setup(){
	eag.experiences = make(map[uint64][][]string)
}

func (eag * experienceAgent) AddStep(id uint64, step []string){
	steps, ok := eag.experiences[id]
	if !ok{
		eag.experiences[id] = [][]string{step,}
	}else{
		steps = append(steps, step)
		eag.experiences[id] = steps
	}
}

func (eag * experienceAgent) CloseExperience(id uint64){
	if steps, ok := eag.experiences[id]; ok{

		fileName := fmt.Sprintf("/tmp/episode_%d.csv", id)

		file, err := os.Create(fileName)
		if err != nil{
			panic(err)
		}
		writer := csv.NewWriter(file)
		defer file.Close()

		for _, row := range steps{
			err := writer.Write(row)
			if err != nil{
				panic(err)
			}
		}
		writer.Flush()
	}
}
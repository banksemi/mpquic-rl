package agents

import (
	"encoding/csv"
	"fmt"
	"os"
)

func writeEpisode(buffer [][]string, episodeID uint64, path string) {

	fileName := fmt.Sprintf(path+"/episode_%d.csv", episodeID)

	file, err := os.Create(fileName)
	if err != nil {
		panic(err)
	}
	writer := csv.NewWriter(file)
	defer file.Close()

	for _, row := range buffer {
		err := writer.Write(row)
		if err != nil {
			panic(err)
		}
	}

	writer.Flush()
}

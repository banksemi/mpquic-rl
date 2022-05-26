package agents

import "testing"

func TestWriteEpisode(t *testing.T) {
	defer recoverFromPanic(t)
	writeEpisode([][]string{}, 1, "./noexistingpath")
}

func recoverFromPanic(t *testing.T) {
	if r := recover(); r == nil {
		t.Error("Test did not panic")
	}
}

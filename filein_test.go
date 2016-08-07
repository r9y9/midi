package midi

import (
	"fmt"
	"testing"
)

func TestMIDIFileReader(t *testing.T) {
	m, err := ReadMIDI("test.mid")
	if err != nil {
		t.Error(err)
	}

	return
	fmt.Println("start debug")
	track := 1
	var accumulate uint64 = 0
	for {
		ticks, event := m.NextEvent(track)
		if event == nil {
			break
		}

		accumulate += ticks
		fmt.Print(accumulate, " ")
		for i := range event {
			fmt.Print(event[i], " ")
		}
		fmt.Println("")

	}

	fmt.Println("end debug")
}

func TestMIDIData(t *testing.T) {
	m, err := ReadMIDI("test.mid")
	if err != nil {
		t.Error(err)
	}

	data := BuildMIDIDataFromMIDIFile(m)

	track := data.At(0)
	fmt.Println(track.At(0))
}

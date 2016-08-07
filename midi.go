package midi

import (
	"fmt"
)

// MIDIEvent represents a MIDI Event.
type MIDIEvent struct {
	tick    int64 // absolute tick
	message []uint8
}

func (e *MIDIEvent) Tick() int64 {
	return e.tick
}

func (e *MIDIEvent) Len() int {
	return len(e.message)
}

func (e *MIDIEvent) Message() []uint8 {
	return e.message
}

// MIDITrack represents a MIDI track that is composed of MIDI events.
type MIDITrack struct {
	Name   string
	events []*MIDIEvent
}

func (t *MIDITrack) Append(e *MIDIEvent) {
	t.events = append(t.events, e)
}

func (t *MIDITrack) Len() int {
	return len(t.events)
}

func (t *MIDITrack) At(i int) *MIDIEvent {
	return t.events[i]
}

// MIDIData represents a MIDI data that is composed of MIDI tracks.
type MIDIData struct {
	Name          string
	Format        int
	Division      int
	tracks        []*MIDITrack
	tempoEvents   []TempoChange
	timeSigEvents []TimeSignature
}

func (d *MIDIData) Append(track *MIDITrack) {
	d.tracks = append(d.tracks, track)
}

func (d *MIDIData) At(n int) *MIDITrack {
	return d.tracks[n]
}

func (d *MIDIData) Len() int {
	return len(d.tracks)
}

func BuildMIDIDataFromMIDIFile(m *MIDIFile) *MIDIData {
	d := &MIDIData{
		Division: m.Division,
		Format:   m.Format,
	}

	numTracks := m.NumTracks
	for track := 0; track < numTracks; track++ {
		t := &MIDITrack{}

		var accumulateTicks int64 = 0

		for {
			tick, rawEvent := m.NextEvent(track)
			if rawEvent == nil {
				break
			}
			accumulateTicks += int64(tick)
			event := &MIDIEvent{
				tick:    accumulateTicks,
				message: rawEvent,
			}
			fmt.Println(*event, event.Len())

			t.Append(event)
		}
		d.Append(t)
	}

	return d
}

package midi

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"io/ioutil"
	"os"
)

// References:
// - MIDI file Format: http://www.sonicspot.com/guide/midifiles.html

// MIDIFile represents a standard MIDI file.
// This is a row-level interface to access MIDI data from file.
type MIDIFile struct {
	NumTracks     int
	Format        int
	Division      int
	UsingTimeCode bool

	tickSeconds     []float64
	trackPointers   []int64
	trackOffsets    []int64
	trackLengths    []int64
	trackStatus     []byte
	tempoEvents     []TempoChange
	trackCounters   []uint64
	trackTempoIndex []int
	rawData         []byte
}

type TimeSignature struct {
	Count      uint64
	BeatPerBar int
}

// TempoChanage represents a tempo change event.
type TempoChange struct {
	Count       uint64  // tick
	TickSeconds float64 // tick per seconds
}

func ReadMIDI(filename string) (*MIDIFile, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return Read(file)
}

// Read reads MIDI data from an io.Reader.
func Read(r io.Reader) (*MIDIFile, error) {
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}

	m := &MIDIFile{
		rawData: b,
	}

	err = m.parseRawData()
	if err != nil {
		return nil, err
	}

	return m, nil
}

func (m *MIDIFile) parseRawData() error {
	if m.rawData == nil {
		return errors.New("raw data must be non-nil")
	}

	// just alias
	b := m.rawData

	chunkType := string(b[0:4])
	if chunkType != "MThd" {
		return errors.New("invalid header: " + chunkType +
			". Expected to be MThd.")
	}

	// NOTE that MIDI files are BIG endians.
	// http://www.music.mcgill.ca/~gary/306/week9/smf.html
	var length int32
	binary.Read(bytes.NewReader(b[4:8]), binary.BigEndian, &length)

	if length != 6 {
		return errors.New("doesn't appear to be a MIDI file.")
	}

	// Read the MIDI file format.
	var format int16
	binary.Read(bytes.NewReader(b[8:10]), binary.BigEndian, &format)

	if format < 0 || format > 2 {
		return errors.New("invalid format: " + string(format))
	}
	m.Format = int(format)

	// Read the number of tracks
	var numTracks int16
	binary.Read(bytes.NewReader(b[10:12]), binary.BigEndian, &numTracks)
	m.NumTracks = int(numTracks)

	if format == 0 && numTracks != 1 {
		return errors.New("invalid number of tracks (>0) for a file format = 0! ")
	}

	// Read the beat division.
	var division int16
	binary.Read(bytes.NewReader(b[12:14]), binary.BigEndian, &division)
	m.Division = int(division)

	var tickrate float64
	m.UsingTimeCode = false

	// a bit complecated..
	// see http://www.sonicspot.com/guide/midifiles.html what this code do
	if m.Division&0x8000 > 0 {
		// Determine ticks per second from time-code formats.
		tickrate = float64(-int(m.Division) & 0x7F00)
		// If frames per second value is 29, it really should be 29.97.
		if tickrate == 29.0 {
			tickrate = 29.97
		}
		tickrate *= float64(m.Division & 0x00FF)
		m.UsingTimeCode = true
	} else {
		tickrate = float64(m.Division & 0x00FF)
	}

	// Now locate the track offsets and lengths.  If not using time
	// code, we can initialize the "tick time" using a default tempo of
	// 120 beats per minute.  We will then check for tempo meta-events
	// afterward.
	var bitIndex int64 = 14
	m.tickSeconds = make([]float64, m.NumTracks)
	m.trackPointers = make([]int64, m.NumTracks)
	m.trackOffsets = make([]int64, m.NumTracks)
	m.trackLengths = make([]int64, m.NumTracks)
	m.trackStatus = make([]byte, m.NumTracks)
	for i := 0; i < m.NumTracks; i++ {
		chunkType := string(b[bitIndex : bitIndex+4])
		if chunkType != "MTrk" {
			return errors.New("invalid track header: " + chunkType +
				". Expected to be MTrk.")
		}
		bitIndex += 4

		var length int32
		binary.Read(bytes.NewReader(b[bitIndex:bitIndex+4]),
			binary.BigEndian, &length)
		bitIndex += 4

		m.trackLengths[i] = int64(length)
		m.trackOffsets[i] = int64(bitIndex)
		m.trackPointers[i] = int64(bitIndex)
		m.trackStatus[i] = 0

		if m.UsingTimeCode {
			m.tickSeconds[i] = float64(1.0 / tickrate)
		} else {
			m.tickSeconds[i] = float64(0.5 / tickrate)
		}

		bitIndex += int64(length)
	}

	// Save the initial tickSeconds parameter.
	tempoEvent := TempoChange{
		Count:       0,
		TickSeconds: m.tickSeconds[0],
	}
	m.tempoEvents = append(m.tempoEvents, tempoEvent)

	// If format 1 and not using time code, parse and save the tempo map
	// on track 0.
	if m.Format == 1 && !m.UsingTimeCode {
		var count uint64
		var event []byte

		// We need to temporarily change the usingTimeCode_ value here so
		// that the getNextEvent() function doesn't try to check the tempo
		// map (which we're creating here).
		m.UsingTimeCode = true
		count, event = m.NextEvent(0)

		for {
			if event == nil {
				break
			}
			if len(event) == 6 && event[0] == 0xFF &&
				event[1] == 0x51 && event[2] == 0x03 {
				tempoEvent.Count = count
				value := event[3]<<16 + event[4]<<8 + event[5]
				tempoEvent.TickSeconds = float64(0.000001 *
					float64(value) / tickrate)
				tail := len(m.tempoEvents) - 1
				if count > m.tempoEvents[tail].Count {
					m.tempoEvents = append(m.tempoEvents, tempoEvent)
				} else {
					m.tempoEvents[tail] = tempoEvent
				}
			}
			var countNew uint64
			countNew, event = m.NextEvent(0)
			count += countNew
		}
		m.RewindTrack(0)
		m.trackCounters = make([]uint64, m.NumTracks)
		m.trackTempoIndex = make([]int, m.NumTracks)
		for i := 0; i < m.NumTracks; i++ {
			m.trackCounters[i] = 0
			m.trackTempoIndex[i] = 0
		}

		// Change the time code flag back!
		m.UsingTimeCode = false
	}

	return nil
}

func (m *MIDIFile) NextEvent(track int) (uint64, []byte) {
	if track >= m.NumTracks {
		panic("invalid track number")
	}

	var event []byte

	if m.trackPointers[track]-m.trackOffsets[track] >= m.trackLengths[track] {
		return 0, nil
	}

	var ticks, b uint64
	var position uint64
	isTempoEvent := false

	// Read the event delta time.
	bitIndex, err := m.readVariableLength(&ticks, m.trackPointers[track])
	if err != nil {
		panic(err)
	}

	// Parse the event stream to determine the event length.
	c := m.rawData[bitIndex : bitIndex+1][0]
	bitIndex += 1

	switch c {
	case 0xFF: // A Meta-Event
		m.trackStatus[track] = 0
		event = append(event, c)
		c = m.rawData[bitIndex : bitIndex+1][0]
		bitIndex += 1
		event = append(event, c)
		if m.Format != 1 && c == 0x51 {
			isTempoEvent = true
		}
		position = uint64(bitIndex)

		bitIndex, err := m.readVariableLength(&b, bitIndex)
		if err != nil {
			panic(err)
		}
		b += uint64(uint64(bitIndex) - position)
		bitIndex = int64(position)

	// The start or continuation of a Sysex event
	case 0xF0 | 0xF1 | 0xF2 | 0xF3 | 0xF4 | 0xF5 | 0xF6 | 0xF7:
		m.trackStatus[track] = 0
		event = append(event, c)
		position = uint64(bitIndex)

		bitIndex, err := m.readVariableLength(&b, bitIndex)
		if err != nil {
			panic(err)
		}
		b += uint64(uint64(bitIndex) - position)
		bitIndex = int64(position)

	// Should be a MIDI channel event
	default:
		if c&0x80 > 0 {
			if c > 0xF0 {
				panic("invlid midi channel event")
			}
			m.trackStatus[track] = c
			event = append(event, c)
			c &= 0xF0
			if c == 0xC0 || c == 0xD0 {
				b = 1
			} else {
				b = 2
			}
		} else if m.trackStatus[track]&0x80 == 1 {
			event = append(event, m.trackStatus[track])
			event = append(event, c)
			c = m.trackStatus[track] & 0xF0
			if c != 0xC0 && c != 0xD0 {
				b = 1
			}
		} else {
			panic("invalid midi channel event; never reach here.")
		}
	}

	// Read the rest of the event into the event vector.
	for i := 0; i < int(b); i++ {
		c := m.rawData[bitIndex : bitIndex+1][0]
		bitIndex += 1
		event = append(event, c)
	}

	if !m.UsingTimeCode {
		if isTempoEvent {
			// Parse the tempo event and update tickSeconds_[track].
			tickrate := float64(m.Division & 0x7FFF)
			value := event[3]<<16 + event[4]<<8 + event[5]
			m.tickSeconds[track] = float64(0.000001 * float64(value) /
				tickrate)
		}

		if m.Format == 1 {
			m.trackCounters[track] += ticks
			tempoEvent := m.tempoEvents[m.trackTempoIndex[track]]
			if m.trackCounters[track] >= tempoEvent.Count &&
				m.trackTempoIndex[track] < len(m.tempoEvents)-1 {
				m.trackTempoIndex[track] += 1
				m.tickSeconds[track] = tempoEvent.TickSeconds
			}
		}
	}

	// Save the current track pointer value.
	m.trackPointers[track] = bitIndex

	return ticks, event
}

func (m *MIDIFile) NextMIDIEvent(track int) (uint64, []byte) {
	if track >= m.NumTracks {
		panic("invalid track argmnent")
	}

	ticks, midiEvent := m.NextEvent(track)

	for {
		if midiEvent == nil || midiEvent[0] < 0xF0 {
			break
		}
		ticks, midiEvent = m.NextEvent(track)
	}

	return ticks, midiEvent
}

func (m *MIDIFile) RewindTrack(track int) {
	if track >= m.NumTracks {
		panic("invalid track argmnent")
	}

	m.trackPointers[track] = m.trackOffsets[track]
	m.trackStatus[track] = 0
	m.tickSeconds[track] = m.tempoEvents[0].TickSeconds
}

func (m *MIDIFile) TickSeconds(track int) float64 {
	if track >= m.NumTracks {
		panic("invalid track argmnent")
	}

	return m.tickSeconds[track]
}

func (m *MIDIFile) readVariableLength(val *uint64, bitIndex int64) (int64, error) {
	*val = 0
	c := m.rawData[bitIndex : bitIndex+1][0]
	*val = uint64(c)
	bitIndex += 1

	if *val&0x80 > 0 {
		*val &= 0x7F
		for {
			if bitIndex >= int64(len(m.rawData)) {
				return 0, errors.New("error")
			}
			c = m.rawData[bitIndex : bitIndex+1][0]
			bitIndex += 1

			*val = (*val << 7) + uint64(c&0x7F)
			if c&0x80 == 0 {
				break
			}
		}
	}

	return bitIndex, nil
}

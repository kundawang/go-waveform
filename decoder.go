package waveform

import (
	"encoding/binary"
	"errors"
	"math"
)

// minWavHeaderSize is the smallest slice that can hold the fixed RIFF/WAVE
// header fields plus the fmt chunk fields DecodeWav reads.
const minWavHeaderSize = 36

// Parser type
type Parser func([]byte) float64

// DecodeWav decodes wav file information from bytes.
//
// A truncated input never panics: when the header or chunks are cut short,
// every read is clamped to the bytes actually present, so a truncated wav
// yields a Wav with the data that could be read and an empty Data slice when
// no data chunk is reachable.
func DecodeWav(bytes []byte) *Wav {
	w := &Wav{}

	if len(bytes) < minWavHeaderSize {
		return w
	}

	w.WaveFormat = WaveFormat(binary.LittleEndian.Uint16(bytes[20:22]))
	w.NumChannels = binary.LittleEndian.Uint16(bytes[22:24])
	w.SampleRate = binary.LittleEndian.Uint32(bytes[24:28])
	w.BitsPerSample = binary.LittleEndian.Uint16(bytes[34:36])

	subchunk1Size := binary.LittleEndian.Uint32(bytes[16:20])

	subchunk2Start := 20 + int(subchunk1Size)

	// Need 8 bytes for the next chunk header (id + size).
	if subchunk2Start+8 > len(bytes) {
		return w
	}

	subchunk2ID := string(bytes[subchunk2Start : subchunk2Start+4])
	subchunk2Size := binary.LittleEndian.Uint32(bytes[subchunk2Start+4 : subchunk2Start+8])

	dataStart := subchunk2Start + 8
	dataSize := int(subchunk2Size)

	if subchunk2ID == "fact" {
		subchunk3Start := subchunk2Start + 8 + int(subchunk2Size)

		if subchunk3Start+8 > len(bytes) {
			return w
		}

		subchunk3Size := binary.LittleEndian.Uint32(bytes[subchunk3Start+4 : subchunk3Start+8])

		dataStart = subchunk3Start + 8
		dataSize = int(subchunk3Size)
	}

	// Clamp to the bytes actually present so a truncated data chunk returns
	// the available samples instead of slicing past the end.
	if dataStart > len(bytes) {
		return w
	}

	if dataStart+dataSize > len(bytes) {
		dataSize = len(bytes) - dataStart
	}

	w.DataChuckSize = uint32(dataSize)
	w.Data = bytes[dataStart : dataStart+dataSize]

	return w
}

// 0 to 255
func int8BitsParser(b []byte) float64 {
	return float64(b[0])
}

// -32768 to 32767
func int16BitsParser(b []byte) float64 {
	value := int16(binary.LittleEndian.Uint16(b))
	return float64(value)
}

func int32BitsParser(b []byte) float64 {
	value := int32(binary.LittleEndian.Uint32(b))
	return float64(value)
}

func float32BitsParser(b []byte) float64 {
	bits := binary.LittleEndian.Uint32(b)
	value := math.Float32frombits(bits)
	return float64(value)
}

func float64BitsParser(b []byte) float64 {
	bits := binary.LittleEndian.Uint64(b)
	return math.Float64frombits(bits)
}

// GetSampleParser get sample parser
func GetSampleParser(bitsPerSample uint16, waveFormat WaveFormat) (func([]byte) float64, error) {
	if waveFormat == WaveFormatPCM {
		if bitsPerSample == 8 {
			return int8BitsParser, nil
		}

		if bitsPerSample == 16 {
			return int16BitsParser, nil
		}

		if bitsPerSample == 32 {
			return int32BitsParser, nil
		}
	}

	if waveFormat == WaveFormatIEEEFloat {
		if bitsPerSample == 32 {
			return float32BitsParser, nil
		}

		if bitsPerSample == 64 {
			return float64BitsParser, nil
		}
	}

	return nil, errors.New("format not support")
}

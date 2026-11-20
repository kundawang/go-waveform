// Command gen-fixtures writes the deterministic fixed wav files used to
// manually verify the waveform pyramid:
//
//   - spike.wav: long mono 16-bit PCM silence containing one full-scale
//     single-sample spike (the transient that naive bucketing loses).
//   - silence.wav: long mono 16-bit PCM pure silence.
package main

import (
	"encoding/binary"
	"log"
	"os"
	"path/filepath"
)

const (
	numChannels   = 1
	sampleRate    = 44100
	bitsPerSample = 16
	numSamples    = 44100 * 10 // 10 minutes
	spikeIndex    = 44100*7 + 123
)

func writeWav(path string, samples []int16) error {
	dataSize := len(samples) * 2

	buf := make([]byte, 44+dataSize)

	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], uint32(36+dataSize))
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16)
	binary.LittleEndian.PutUint16(buf[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(buf[22:24], numChannels)
	binary.LittleEndian.PutUint32(buf[24:28], sampleRate)
	binary.LittleEndian.PutUint32(buf[28:32], sampleRate*numChannels*bitsPerSample/8)
	binary.LittleEndian.PutUint16(buf[32:34], numChannels*bitsPerSample/8)
	binary.LittleEndian.PutUint16(buf[34:36], bitsPerSample)
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:44], uint32(dataSize))

	for i, s := range samples {
		binary.LittleEndian.PutUint16(buf[44+2*i:46+2*i], uint16(s))
	}

	return os.WriteFile(path, buf, 0o644)
}

func main() {
	out := os.Args[1]

	silence := make([]int16, numSamples)

	if err := writeWav(filepath.Join(out, "silence.wav"), silence); err != nil {
		log.Fatal(err)
	}

	spike := make([]int16, numSamples)
	spike[spikeIndex] = 32767

	if err := writeWav(filepath.Join(out, "spike.wav"), spike); err != nil {
		log.Fatal(err)
	}

	log.Printf("wrote silence.wav and spike.wav (%d samples each, spike at index %d)", numSamples, spikeIndex)
}

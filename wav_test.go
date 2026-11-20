package waveform

import (
	"encoding/binary"
	"testing"
)

// buildWav constructs an in-memory PCM wav byte slice. frames interleaves
// channels exactly as a wav data chunk does.
func buildWav(t *testing.T, numChannels uint16, sampleRate uint32, bitsPerSample uint16, frames []byte) []byte {
	t.Helper()

	dataSize := uint32(len(frames))
	riffSize := uint32(36) + dataSize
	byteRate := sampleRate * uint32(numChannels) * uint32(bitsPerSample) / 8
	blockAlign := numChannels * bitsPerSample / 8

	buf := make([]byte, 44+len(frames))

	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], riffSize)
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16)
	binary.LittleEndian.PutUint16(buf[20:22], uint16(WaveFormatPCM))
	binary.LittleEndian.PutUint16(buf[22:24], numChannels)
	binary.LittleEndian.PutUint32(buf[24:28], sampleRate)
	binary.LittleEndian.PutUint32(buf[28:32], byteRate)
	binary.LittleEndian.PutUint16(buf[32:34], blockAlign)
	binary.LittleEndian.PutUint16(buf[34:36], bitsPerSample)
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:44], dataSize)
	copy(buf[44:], frames)

	return buf
}

func int16Frame(values ...int16) []byte {
	b := make([]byte, 2*len(values))

	for i, v := range values {
		binary.LittleEndian.PutUint16(b[2*i:2*i+2], uint16(v))
	}

	return b
}

// TestDecodeWavRoundTrip checks normal mono/stereo decoding and that an odd
// sample rate (non-integral ratio to common render widths) is preserved.
func TestDecodeWavRoundTrip(t *testing.T) {
	frames := append(int16Frame(-1000, 2000), int16Frame(3000, -4000)...)
	frames = append(frames, int16Frame(5000, 6000)...)

	raw := buildWav(t, 2, 44099, 16, frames) // unusual rate on purpose

	w := DecodeWav(raw)

	if w.NumChannels != 2 || w.SampleRate != 44099 || w.BitsPerSample != 16 {
		t.Fatalf("header decode wrong: %+v", w)
	}

	if int(w.DataChuckSize) != len(frames) || len(w.Data) != len(frames) {
		t.Fatalf("data chunk decode wrong: size=%d len=%d", w.DataChuckSize, len(w.Data))
	}

	data, err := w.GetData()
	if err != nil {
		t.Fatalf("GetData: %v", err)
	}

	stereo, ok := data.(*StereoData)
	if !ok {
		t.Fatalf("got %T, want *StereoData", data)
	}

	wantL := Sample{-1000, 3000, 5000}
	wantR := Sample{2000, -4000, 6000}

	if len(stereo.LSample) != 3 || len(stereo.RSample) != 3 {
		t.Fatalf("channel lengths = %d/%d, want 3/3", len(stereo.LSample), len(stereo.RSample))
	}

	for i := range wantL {
		if stereo.LSample[i] != wantL[i] || stereo.RSample[i] != wantR[i] {
			t.Fatalf("frame %d = (%v,%v), want (%v,%v)", i, stereo.LSample[i], stereo.RSample[i], wantL[i], wantR[i])
		}
	}

	// Mono path, odd sample count.
	monoFrames := int16Frame(7)
	monoFrames = append(monoFrames, int16Frame(-8)...)
	monoFrames = append(monoFrames, int16Frame(9)...)

	mono := DecodeWav(buildWav(t, 1, 8000, 16, monoFrames))

	mdata, err := mono.GetData()
	if err != nil {
		t.Fatalf("GetData mono: %v", err)
	}

	monoSamples := mdata.(*MonoData).Sample
	if len(monoSamples) != 3 || monoSamples[0] != 7 || monoSamples[1] != -8 || monoSamples[2] != 9 {
		t.Fatalf("mono decode wrong: %v", monoSamples)
	}
}

// TestDecodeWavTruncatedData cuts a stereo wav mid-frame. The decoder must
// keep the complete frames, keep the channels equal length, and never panic.
func TestDecodeWavTruncatedData(t *testing.T) {
	frames := append(int16Frame(1, 2), int16Frame(3, 4)...)
	frames = append(frames, int16Frame(5, 6)...)
	frames = append(frames, int16Frame(7, 8)...)

	raw := buildWav(t, 2, 44100, 16, frames)

	// Cut 3 bytes off the end: the last frame loses its right channel.
	cut := raw[:len(raw)-3]

	w := DecodeWav(cut)

	if w.DataChuckSize != uint32(len(frames)-3) {
		t.Fatalf("data size = %d, want %d", w.DataChuckSize, len(frames)-3)
	}

	data, err := w.GetData()
	if err != nil {
		t.Fatalf("GetData on truncated wav: %v", err)
	}

	stereo := data.(*StereoData)

	if len(stereo.LSample) != len(stereo.RSample) {
		t.Fatalf("truncated channels diverge: %d vs %d", len(stereo.LSample), len(stereo.RSample))
	}

	if len(stereo.LSample) != 3 {
		t.Fatalf("truncated frame count = %d, want 3 complete frames", len(stereo.LSample))
	}

	// Truncated mono wav with one dangling byte: dropped cleanly.
	monoFrames := []byte{128, 200, 50} // 8-bit mono: 3 bytes
	monoRaw := buildWav(t, 1, 8000, 8, monoFrames)

	monoCut := monoRaw[:len(monoRaw)-1]

	mw := DecodeWav(monoCut)
	mdata, err := mw.GetData()
	if err != nil {
		t.Fatalf("GetData truncated mono: %v", err)
	}

	if got := len(mdata.(*MonoData).Sample); got != 2 {
		t.Fatalf("truncated mono samples = %d, want 2", got)
	}
}

// TestDecodeWavTruncatedHeader covers files cut inside the header or at a
// chunk boundary: defined empty result, no slice-out-of-range panic.
func TestDecodeWavTruncatedHeader(t *testing.T) {
	for _, size := range []int{0, 1, 10, 35, 36, 43} {
		raw := make([]byte, size)

		w := DecodeWav(raw)

		if w == nil {
			t.Fatalf("size %d: nil wav", size)
		}

		if len(w.Data) != 0 {
			t.Fatalf("size %d: unexpected data %d bytes", size, len(w.Data))
		}
	}
}

// TestDecodeWavDeclaredSizeExceedsBytes covers a data-chunk header that
// promises more bytes than the file contains: clamp to what is present.
func TestDecodeWavDeclaredSizeExceedsBytes(t *testing.T) {
	frames := int16Frame(1, 2)
	raw := buildWav(t, 2, 44100, 16, frames)

	// Lie about the data size: claim 1000 bytes while only 4 exist.
	binary.LittleEndian.PutUint32(raw[40:44], 1000)

	w := DecodeWav(raw)

	if len(w.Data) != len(frames) {
		t.Fatalf("data not clamped: got %d bytes, want %d", len(w.Data), len(frames))
	}

	if w.DataChuckSize != uint32(len(frames)) {
		t.Fatalf("DataChuckSize = %d, want %d", w.DataChuckSize, len(frames))
	}

	data, err := w.GetData()
	if err != nil {
		t.Fatalf("GetData: %v", err)
	}

	stereo := data.(*StereoData)
	if len(stereo.LSample) != 1 || stereo.LSample[0] != 1 || stereo.RSample[0] != 2 {
		t.Fatalf("decoded frame wrong: %+v", stereo)
	}
}

// TestGetDataEmptyData covers a well-formed header with an empty data chunk.
func TestGetDataEmptyData(t *testing.T) {
	raw := buildWav(t, 1, 44100, 16, nil)

	w := DecodeWav(raw)

	data, err := w.GetData()
	if err != nil {
		t.Fatalf("GetData empty: %v", err)
	}

	mono := data.(*MonoData)
	if len(mono.Sample) != 0 {
		t.Fatalf("empty data produced %d samples", len(mono.Sample))
	}
}

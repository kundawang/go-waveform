package image

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cettoana/go-waveform"
)

func boundPCM16() *waveform.Bound {
	return &waveform.Bound{Upper: 32767, Lower: -32768}
}

func TestOutputWaveformImageMonoAndStereo(t *testing.T) {
	dir := t.TempDir()

	samples := make(waveform.Sample, 5000)
	for i := range samples {
		samples[i] = float64((i % 7) * 100)
	}

	samples[2500] = 30000 // transient that the pyramid must preserve

	mono := &waveform.MonoData{Sample: samples, Bound: boundPCM16()}

	monoPath := filepath.Join(dir, "mono")
	if err := OutputWaveformImage(mono, &Option{
		FileName:   monoPath,
		FileType:   "png",
		Resolution: 200,
		Width:      800,
		Theme:      "dark",
	}); err != nil {
		t.Fatalf("render mono: %v", err)
	}

	if fi, err := os.Stat(monoPath + ".png"); err != nil || fi.Size() == 0 {
		t.Fatalf("mono png missing or empty: %v", err)
	}

	stereo := &waveform.StereoData{
		LSample: samples,
		RSample: append(waveform.Sample{}, samples...),
		Bound:   boundPCM16(),
	}

	stereoPath := filepath.Join(dir, "stereo")
	if err := OutputWaveformImage(stereo, &Option{
		FileName:   stereoPath,
		FileType:   "svg",
		Resolution: 100,
		Theme:      "light",
	}); err != nil {
		t.Fatalf("render stereo: %v", err)
	}

	for _, postfix := range []string{"-L", "-R"} {
		if fi, err := os.Stat(stereoPath + postfix + ".svg"); err != nil || fi.Size() == 0 {
			t.Fatalf("stereo%s svg missing or empty: %v", postfix, err)
		}
	}
}

// TestOutputOriginalWaveformImageCompatibility keeps the legacy direct-drawing
// entry working with both fast and full modes.
func TestOutputOriginalWaveformImageCompatibility(t *testing.T) {
	dir := t.TempDir()

	samples := make(waveform.Sample, 2000)
	for i := range samples {
		samples[i] = float64(i % 100)
	}

	mono := &waveform.MonoData{Sample: samples, Bound: boundPCM16()}

	for _, fast := range []bool{false, true} {
		path := filepath.Join(dir, "original")

		if err := OutputWaveformImage(mono, &Option{
			FileName:   path,
			FileType:   "png",
			Style:      "original",
			Fast:       fast,
			Resolution: 100,
		}); err != nil {
			t.Fatalf("original render fast=%v: %v", fast, err)
		}

		if fi, err := os.Stat(path + ".png"); err != nil || fi.Size() == 0 {
			t.Fatalf("original png fast=%v missing or empty: %v", fast, err)
		}
	}
}

// TestOutputWaveformImageEmpty covers zero-length samples: a defined empty
// render instead of a panic or division by zero.
func TestOutputWaveformImageEmpty(t *testing.T) {
	dir := t.TempDir()

	mono := &waveform.MonoData{Sample: waveform.Sample{}, Bound: boundPCM16()}
	path := filepath.Join(dir, "empty")

	if err := OutputWaveformImage(mono, &Option{
		FileName:   path,
		FileType:   "png",
		Resolution: 100,
	}); err != nil {
		t.Fatalf("render empty: %v", err)
	}

	if fi, err := os.Stat(path + ".png"); err != nil || fi.Size() == 0 {
		t.Fatalf("empty png missing or empty: %v", err)
	}
}

// TestOutputPyramidImage exercises the reusable prebuilt-pyramid entry and
// confirms level selection keeps a single-sample spike visible at overview
// resolutions.
func TestOutputPyramidImage(t *testing.T) {
	dir := t.TempDir()

	const n = 1 << 14

	samples := make(waveform.Sample, n)
	samples[42] = 32000

	pyramid, err := waveform.BuildPyramid(samples)
	if err != nil {
		t.Fatalf("BuildPyramid: %v", err)
	}

	// Spike present in the level chosen for a very narrow overview render.
	level := pyramid.LevelForWidth(8)

	found := false

	for _, b := range level.Buckets {
		if b.Max == 32000 {
			found = true
			break
		}
	}

	if !found {
		t.Fatal("overview level lost the single-sample spike")
	}

	path := filepath.Join(dir, "pyramid")

	if err := OutputPyramidImage(pyramid, boundPCM16(), &Option{
		FileName:   path,
		FileType:   "png",
		Resolution: 8,
		Width:      400,
		Theme:      "dark",
	}, ""); err != nil {
		t.Fatalf("OutputPyramidImage: %v", err)
	}

	if fi, err := os.Stat(path + ".png"); err != nil || fi.Size() == 0 {
		t.Fatalf("pyramid png missing or empty: %v", err)
	}
}

// TestOutputWaveformImageMorePixelsThanSamples renders a tiny sample set into
// many pixels: empty trailing envelopes must not produce stray origin lines or
// errors.
func TestOutputWaveformImageMorePixelsThanSamples(t *testing.T) {
	dir := t.TempDir()

	mono := &waveform.MonoData{
		Sample: waveform.Sample{10, -20, 30},
		Bound:  boundPCM16(),
	}
	path := filepath.Join(dir, "wide")

	if err := OutputWaveformImage(mono, &Option{
		FileName:   path,
		FileType:   "svg",
		Resolution: 100,
	}); err != nil {
		t.Fatalf("render over-width: %v", err)
	}

	if fi, err := os.Stat(path + ".svg"); err != nil || fi.Size() == 0 {
		t.Fatalf("over-width svg missing or empty: %v", err)
	}
}

// TestOutputWaveformImageUnsupportedData keeps the legacy error contract.
func TestOutputWaveformImageUnsupportedData(t *testing.T) {
	err := OutputWaveformImage("not a wav", &Option{
		FileName:   filepath.Join(t.TempDir(), "x"),
		FileType:   "png",
		Resolution: 10,
	})

	if err == nil {
		t.Fatal("expected error for unsupported data type")
	}
}

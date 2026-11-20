// Command verify-fixtures decodes the fixed spike/silence wav files with the
// waveform library and audits the pyramid: every level's envelope, the
// retained short tail, and the pixel-level render positions at an overview
// width. It exits non-zero on the first failed check.
package main

import (
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"

	"github.com/cettoana/go-waveform"
)

const overviewWidth = 20

func main() {
	dir := os.Args[1]

	if err := verifySpike(filepath.Join(dir, "spike.wav")); err != nil {
		log.Fatalf("spike.wav: %v", err)
	}

	if err := verifySilence(filepath.Join(dir, "silence.wav")); err != nil {
		log.Fatalf("silence.wav: %v", err)
	}

	log.Print("all fixture checks passed")
}

func loadMono(path string) (*waveform.MonoData, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	w := waveform.DecodeWav(raw)

	data, err := w.GetData()
	if err != nil {
		return nil, err
	}

	mono, ok := data.(*waveform.MonoData)
	if !ok {
		return nil, fmt.Errorf("expected mono data, got %T", data)
	}

	return mono, nil
}

func verifySpike(path string) error {
	mono, err := loadMono(path)
	if err != nil {
		return err
	}

	samples := mono.Sample
	n := len(samples)

	spikeIndex := -1

	for i, v := range samples {
		if v != 0 {
			if spikeIndex != -1 {
				return fmt.Errorf("more than one non-zero sample: %d and %d", spikeIndex, i)
			}

			spikeIndex = i
		}
	}

	if spikeIndex == -1 {
		return fmt.Errorf("no spike found")
	}

	log.Printf("spike.wav: %d samples, single spike at index %d (value %v)", n, spikeIndex, samples[spikeIndex])

	pyramid, err := waveform.BuildPyramid(samples)
	if err != nil {
		return err
	}

	for li, level := range pyramid.Levels {
		var minV, maxV float64

		for _, b := range level.Buckets {
			if b.Max > maxV {
				maxV = b.Max
			}

			if b.Min < minV {
				minV = b.Min
			}

			if b.End <= b.Start {
				return fmt.Errorf("level %d has an empty bucket", li)
			}
		}

		if maxV != samples[spikeIndex] {
			return fmt.Errorf("level %d (bucket size %d) lost the spike: max=%v", li, level.BucketSize, maxV)
		}

		// The bucket carrying the spike must contain the spike's exact index.
		found := false

		for _, b := range level.Buckets {
			if b.Start <= spikeIndex && spikeIndex < b.End {
				found = true

				if b.Max != samples[spikeIndex] {
					return fmt.Errorf("level %d spike bucket max=%v", li, b.Max)
				}

				break
			}
		}

		if !found {
			return fmt.Errorf("level %d has no bucket covering spike index %d", li, spikeIndex)
		}

		log.Printf("  level %2d: bucketSize=%6d buckets=%6d envelope=[%.0f,%.0f] spike retained",
			li, level.BucketSize, len(level.Buckets), minV, maxV)

		if level.Buckets[len(level.Buckets)-1].End != n {
			return fmt.Errorf("level %d dropped the tail", li)
		}
	}

	// Overview render: exactly one pixel should carry the spike, and its
	// position must correspond to the spike's location in time.
	envelopes := pyramid.Aggregate(overviewWidth)

	spikePixel := -1

	for i, env := range envelopes {
		if env.Max == samples[spikeIndex] {
			if spikePixel != -1 {
				return fmt.Errorf("spike appears in two pixels: %d and %d", spikePixel, i)
			}

			spikePixel = i
		}

		if env.Start < 0 || env.End > n || env.Start >= env.End {
			return fmt.Errorf("pixel %d has an invalid range [%d,%d)", i, env.Start, env.End)
		}
	}

	if spikePixel == -1 {
		return fmt.Errorf("spike missing from %d-pixel overview render", overviewWidth)
	}

	wantPixel := spikeIndex * overviewWidth / n
	// Whole-bucket grouping can shift by at most one pixel versus the naive
	// proportional index.
	if delta := spikePixel - wantPixel; math.Abs(float64(delta)) > 1 {
		return fmt.Errorf("spike rendered at pixel %d, want near %d", spikePixel, wantPixel)
	}

	log.Printf("  overview (%d px): spike rendered at pixel %d (expected ~%d), range [%d,%d)",
		overviewWidth, spikePixel, wantPixel, envelopes[spikePixel].Start, envelopes[spikePixel].End)

	return nil
}

func verifySilence(path string) error {
	mono, err := loadMono(path)
	if err != nil {
		return err
	}

	n := len(mono.Sample)

	pyramid, err := waveform.BuildPyramid(mono.Sample)
	if err != nil {
		return err
	}

	log.Printf("silence.wav: %d samples, %d levels, %d total buckets", n, len(pyramid.Levels), pyramid.TotalBuckets)

	for li, level := range pyramid.Levels {
		for bi, b := range level.Buckets {
			if b.Min != 0 || b.Max != 0 {
				return fmt.Errorf("level %d bucket %d non-zero in silence: %+v", li, bi, b)
			}
		}

		if level.Buckets[0].Start != 0 || level.Buckets[len(level.Buckets)-1].End != n {
			return fmt.Errorf("level %d does not cover [0,%d)", li, n)
		}
	}

	envelopes := pyramid.Aggregate(overviewWidth)

	for i, env := range envelopes {
		if env.Min != 0 || env.Max != 0 {
			return fmt.Errorf("overview pixel %d non-zero in silence", i)
		}
	}

	log.Printf("  overview (%d px): all envelopes flat zero, full range covered", overviewWidth)

	return nil
}

package waveform

import (
	"errors"
	"math"
)

// DefaultMaxPyramidBuckets bounds the total number of buckets kept by one
// Pyramid across every level. A bucket is 32 bytes, so the default caps a
// single-channel pyramid at roughly 128 MiB. When an input would exceed the
// cap, the finest level is coarsened (its bucket size grows) instead of
// allocating without bound, so even very long files have a defined result.
const DefaultMaxPyramidBuckets = 4_000_000

// MinMaxBucket is the min/max envelope of the half-open sample interval
// [Start, End). Start and End are indices into the original PCM slice, so
// every level keeps the same time coordinates as the source samples.
type MinMaxBucket struct {
	Start int
	End   int
	Min   float64
	Max   float64
}

// PyramidLevel is one resolution level of a Pyramid. Buckets partition the
// whole sample range in order; the final bucket may be shorter than
// BucketSize when the sample count is not an exact multiple.
type PyramidLevel struct {
	BucketSize int
	Buckets    []MinMaxBucket
}

// Pyramid is a multi-resolution min/max hierarchy built from PCM samples.
// Level 0 is the finest level; each following level doubles the bucket size.
type Pyramid struct {
	Levels     []*PyramidLevel
	NumSamples int
	// NonFinite is the number of NaN/+Inf/-Inf samples found while building
	// level 0. Non-finite samples never participate in min/max aggregation.
	NonFinite int
	// TotalBuckets is the sum of bucket counts over all levels and is
	// guaranteed not to exceed the configured memory cap.
	TotalBuckets int
}

type pyramidConfig struct {
	maxBuckets int
}

// PyramidOption customizes pyramid construction.
type PyramidOption func(*pyramidConfig)

// WithMaxPyramidBuckets overrides the total-bucket memory cap. It must be a
// positive number; a non-positive value makes BuildPyramid fail.
func WithMaxPyramidBuckets(n int) PyramidOption {
	return func(c *pyramidConfig) {
		c.maxBuckets = n
	}
}

// BuildPyramid builds a multi-resolution min/max pyramid from PCM samples.
//
// An empty (nil or zero-length) sample slice produces an empty, non-nil
// pyramid with no levels. Non-finite samples (NaN/+Inf/-Inf) are skipped and
// counted in Pyramid.NonFinite; a bucket that contains only non-finite
// samples gets Min == Max == 0.
func BuildPyramid(samples Sample, options ...PyramidOption) (*Pyramid, error) {
	cfg := pyramidConfig{maxBuckets: DefaultMaxPyramidBuckets}

	for _, opt := range options {
		opt(&cfg)
	}

	if cfg.maxBuckets < 1 {
		return nil, errors.New("pyramid bucket cap must be positive")
	}

	p := &Pyramid{NumSamples: len(samples)}

	if len(samples) == 0 {
		return p, nil
	}

	// Pick the finest-level bucket size so that the sum of bucket counts over
	// all levels (roughly twice the finest-level count) stays under the cap.
	// Rebuild with a doubled bucket size if the estimate overshoots, which
	// makes the cap a hard guarantee rather than a hope.
	bucketSize := firstBucketSize(len(samples), cfg.maxBuckets)

	for {
		level, nonFinite := buildFinestLevel(samples, bucketSize)
		p.Levels = []*PyramidLevel{level}
		p.NonFinite = nonFinite
		p.TotalBuckets = len(level.Buckets)

		for len(p.Levels[len(p.Levels)-1].Buckets) > 1 {
			parent := p.Levels[len(p.Levels)-1]
			child := mergeLevel(parent)
			p.Levels = append(p.Levels, child)
			p.TotalBuckets += len(child.Buckets)
		}

		if p.TotalBuckets <= cfg.maxBuckets {
			return p, nil
		}

		bucketSize *= 2
	}
}

// BuildStereoPyramids builds one pyramid per channel. The left channel is
// always returned first, keeping channel order identical to the source PCM.
func BuildStereoPyramids(left, right Sample, options ...PyramidOption) (*Pyramid, *Pyramid, error) {
	lp, err := BuildPyramid(left, options...)
	if err != nil {
		return nil, nil, err
	}

	rp, err := BuildPyramid(right, options...)
	if err != nil {
		return nil, nil, err
	}

	return lp, rp, nil
}

// LevelForWidth selects the level best suited for drawing into width pixels:
// the finest level that still has at least width buckets, so each pixel can
// cover one or a few whole buckets and no envelope is ever split across a
// pixel boundary. When width is larger than every level's bucket count the
// coarsest level is returned. It returns nil for an empty pyramid or a
// non-positive width.
func (p *Pyramid) LevelForWidth(width int) *PyramidLevel {
	if p == nil || len(p.Levels) == 0 || width <= 0 {
		return nil
	}

	for _, level := range p.Levels {
		if len(level.Buckets) >= width {
			return level
		}
	}

	return p.Levels[len(p.Levels)-1]
}

// Aggregate re-aggregates the pyramid into exactly width min/max envelopes
// for rendering. Whole level buckets are assigned to pixels in contiguous
// groups, so every min/max extreme in the pyramid reaches some pixel and a
// transient can never vanish between pixel boundaries. The returned Start/End
// are real sample indices taken from the underlying bucket spans and the
// envelopes tile the sample range without gaps or overlaps. When width
// exceeds the bucket count the trailing pixels have no bucket and keep
// Min == Max == 0. It returns nil for an empty pyramid or non-positive width.
func (p *Pyramid) Aggregate(width int) []MinMaxBucket {
	if p == nil || len(p.Levels) == 0 || width <= 0 {
		return nil
	}

	level := p.LevelForWidth(width)
	buckets := level.Buckets
	out := make([]MinMaxBucket, width)
	bucketCount := len(buckets)

	for pixel := 0; pixel < width; pixel++ {
		// When there are more pixels than buckets the proportional slices are
		// narrower than one bucket; pin the first pixel to the leading bucket
		// so the waveform is still drawn once, and leave the other empty
		// pixels as zero envelopes.
		if bucketCount < width {
			if pixel != 0 {
				continue
			}

			out[0] = MinMaxBucket{
				Start: buckets[0].Start,
				End:   buckets[bucketCount-1].End,
				Min:   buckets[0].Min,
				Max:   buckets[0].Max,
			}

			for k := 1; k < bucketCount; k++ {
				b := buckets[k]

				if b.Min < out[0].Min {
					out[0].Min = b.Min
				}

				if b.Max > out[0].Max {
					out[0].Max = b.Max
				}
			}

			continue
		}

		first := pixel * bucketCount / width
		endExclusive := (pixel + 1) * bucketCount / width

		// No whole bucket falls into this pixel: leave a zero envelope.
		if first >= bucketCount || endExclusive == first {
			continue
		}

		last := endExclusive - 1

		env := MinMaxBucket{
			Start: buckets[first].Start,
			End:   buckets[last].End,
			Min:   buckets[first].Min,
			Max:   buckets[first].Max,
		}

		for k := first + 1; k <= last; k++ {
			b := buckets[k]

			if b.Min < env.Min {
				env.Min = b.Min
			}

			if b.Max > env.Max {
				env.Max = b.Max
			}

			env.End = b.End
		}

		out[pixel] = env
	}

	return out
}

// firstBucketSize estimates the smallest finest-level bucket size whose
// complete pyramid should fit within cap buckets: each coarser level roughly
// halves the bucket count, so all levels together hold about twice the finest
// count.
func firstBucketSize(numSamples, cap int) int {
	if cap <= 1 {
		return numSamples
	}

	size := (2*numSamples + cap - 1) / cap
	if size < 1 {
		size = 1
	}

	return size
}

// buildFinestLevel scans the raw PCM slice once to produce level 0. The final
// bucket keeps whatever tail remains when numSamples is not an exact multiple
// of bucketSize. Non-finite samples are skipped and counted.
func buildFinestLevel(samples Sample, bucketSize int) (*PyramidLevel, int) {
	n := len(samples)
	count := (n + bucketSize - 1) / bucketSize
	buckets := make([]MinMaxBucket, 0, count)
	nonFinite := 0

	for start := 0; start < n; start += bucketSize {
		end := start + bucketSize
		if end > n {
			end = n
		}

		bucket := MinMaxBucket{Start: start, End: end}
		hasValue := false

		for _, v := range samples[start:end] {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				nonFinite++
				continue
			}

			if !hasValue {
				bucket.Min = v
				bucket.Max = v
				hasValue = true
				continue
			}

			if v < bucket.Min {
				bucket.Min = v
			}

			if v > bucket.Max {
				bucket.Max = v
			}
		}

		if !hasValue {
			bucket.Min = 0
			bucket.Max = 0
		}

		buckets = append(buckets, bucket)
	}

	return &PyramidLevel{BucketSize: bucketSize, Buckets: buckets}, nonFinite
}

// mergeLevel builds the next coarser level by merging consecutive pairs of
// parent buckets, carrying their sample intervals and min/max envelopes. An
// odd trailing bucket is kept as the final bucket so no samples are lost.
func mergeLevel(parent *PyramidLevel) *PyramidLevel {
	parentBuckets := parent.Buckets
	childCount := (len(parentBuckets) + 1) / 2
	children := make([]MinMaxBucket, 0, childCount)

	for i := 0; i < len(parentBuckets); i += 2 {
		first := parentBuckets[i]
		merged := MinMaxBucket{
			Start: first.Start,
			End:   first.End,
			Min:   first.Min,
			Max:   first.Max,
		}

		if i+1 < len(parentBuckets) {
			second := parentBuckets[i+1]
			merged.End = second.End

			if second.Min < merged.Min {
				merged.Min = second.Min
			}

			if second.Max > merged.Max {
				merged.Max = second.Max
			}
		}

		children = append(children, merged)
	}

	return &PyramidLevel{BucketSize: parent.BucketSize * 2, Buckets: children}
}

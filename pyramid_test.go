package waveform

import (
	"math"
	"testing"
)

// envelopeAt returns the min/max a level reports over the samples in [start,end).
func envelopeAt(t *testing.T, level *PyramidLevel, start, end int) (float64, float64) {
	t.Helper()

	minV, maxV := 0.0, 0.0
	found := false

	for _, b := range level.Buckets {
		if b.Start >= end || b.End <= start {
			continue
		}

		if !found {
			minV, maxV = b.Min, b.Max
			found = true
			continue
		}

		if b.Min < minV {
			minV = b.Min
		}

		if b.Max > maxV {
			maxV = b.Max
		}
	}

	if !found {
		t.Fatalf("no bucket covers [%d,%d)", start, end)
	}

	return minV, maxV
}

// TestBuildPyramidSingleSampleSpike is the core regression: a one-sample
// transient must survive every coarser level of the hierarchy, including the
// level used for a one-hour full-overview style render.
func TestBuildPyramidSingleSampleSpike(t *testing.T) {
	const n = 1 << 14 // 16384 samples, non-trivial depth

	samples := make(Sample, n)
	for i := range samples {
		samples[i] = 0.01 // quiet floor
	}

	samples[100] = 100    // positive one-sample spike
	samples[10000] = -100 // negative one-sample spike

	p, err := BuildPyramid(samples)
	if err != nil {
		t.Fatalf("BuildPyramid: %v", err)
	}

	if len(p.Levels) < 2 {
		t.Fatalf("expected multiple levels, got %d", len(p.Levels))
	}

	// Finest level: bucket size 1 (no cap pressure at this size) and the
	// spike bucket carries exactly the spike value.
	finest := p.Levels[0]
	if finest.BucketSize != 1 {
		t.Fatalf("finest bucket size = %d, want 1", finest.BucketSize)
	}

	if finest.Buckets[100].Max != 100 || finest.Buckets[100].Min != 100 {
		t.Fatalf("finest positive spike lost: %+v", finest.Buckets[100])
	}

	if finest.Buckets[10000].Min != -100 || finest.Buckets[10000].Max != -100 {
		t.Fatalf("finest negative spike lost: %+v", finest.Buckets[10000])
	}

	// Every coarser level must still contain both extremes.
	for li, level := range p.Levels {
		minV, maxV := envelopeAt(t, level, 0, n)

		if maxV != 100 {
			t.Fatalf("level %d (bucket size %d): positive spike lost, max = %v", li, level.BucketSize, maxV)
		}

		if minV != -100 {
			t.Fatalf("level %d (bucket size %d): negative spike lost, min = %v", li, level.BucketSize, minV)
		}
	}

	// Geometry invariants on every level.
	for li, level := range p.Levels {
		if len(level.Buckets) == 0 {
			t.Fatalf("level %d has no buckets", li)
		}

		if level.Buckets[0].Start != 0 {
			t.Fatalf("level %d does not start at sample 0", li)
		}

		if level.Buckets[len(level.Buckets)-1].End != n {
			t.Fatalf("level %d does not end at sample %d", li, n)
		}

		for bi, b := range level.Buckets {
			if b.End <= b.Start {
				t.Fatalf("level %d bucket %d has empty range [%d,%d)", li, bi, b.Start, b.End)
			}

			if b.Min > b.Max {
				t.Fatalf("level %d bucket %d min > max", li, bi)
			}

			if bi > 0 && b.Start != level.Buckets[bi-1].End {
				t.Fatalf("level %d bucket %d leaves a gap", li, bi)
			}
		}
	}
}

// TestBuildPyramidLongSilence checks the long-silence fixed fixture: zero
// input collapses to zero envelopes, and bucket indices stay exact even with
// non-divisible tail lengths.
func TestBuildPyramidLongSilence(t *testing.T) {
	const n = 10003 // deliberately not a power of two

	samples := make(Sample, n)

	p, err := BuildPyramid(samples)
	if err != nil {
		t.Fatalf("BuildPyramid: %v", err)
	}

	if p.NumSamples != n {
		t.Fatalf("NumSamples = %d, want %d", p.NumSamples, n)
	}

	for li, level := range p.Levels {
		for bi, b := range level.Buckets {
			if b.Min != 0 || b.Max != 0 {
				t.Fatalf("level %d bucket %d non-zero envelope in silence: %+v", li, bi, b)
			}
		}

		last := level.Buckets[len(level.Buckets)-1]
		if last.End != n {
			t.Fatalf("level %d tail dropped: last end = %d, want %d", li, last.End, n)
		}
	}

	// A spike in the very last (short) bucket of the finest level must still
	// propagate upward — the short tail is retained, not discarded.
	samples[n-1] = 42

	p2, err := BuildPyramid(samples)
	if err != nil {
		t.Fatalf("BuildPyramid: %v", err)
	}

	for li, level := range p2.Levels {
		_, maxV := envelopeAt(t, level, 0, n)

		if maxV != 42 {
			t.Fatalf("level %d lost tail spike, max = %v", li, maxV)
		}
	}
}

// TestBuildPyramidLevelBucketSizesAndOrder verifies the doubling hierarchy
// and the exact bucket partition including the retained short tail.
func TestBuildPyramidLevelBucketSizesAndOrder(t *testing.T) {
	const n = 257 // odd length forces a short tail on every level

	samples := make(Sample, n)
	for i := range samples {
		samples[i] = float64(i)
	}

	p, err := BuildPyramid(samples)
	if err != nil {
		t.Fatalf("BuildPyramid: %v", err)
	}

	for li, level := range p.Levels {
		wantSize := 1 << li
		if level.BucketSize != wantSize {
			t.Fatalf("level %d bucket size = %d, want %d", li, level.BucketSize, wantSize)
		}

		// Buckets cover [0,n) exactly once.
		if level.Buckets[0].Start != 0 {
			t.Fatalf("level %d starts at %d", li, level.Buckets[0].Start)
		}

		for bi, b := range level.Buckets {
			wantEnd := b.Start + wantSize
			if wantEnd > n {
				wantEnd = n
			}

			if b.End != wantEnd {
				t.Fatalf("level %d bucket %d end = %d, want %d", li, bi, b.End, wantEnd)
			}

			// Min/max match the raw samples inside the interval (ramp data).
			if b.Min != float64(b.Start) || b.Max != float64(b.End-1) {
				t.Fatalf("level %d bucket %d wrong envelope: got [%v,%v] want [%d,%d]",
					li, bi, b.Min, b.Max, b.Start, b.End-1)
			}
		}

		if level.Buckets[len(level.Buckets)-1].End != n {
			t.Fatalf("level %d tail not retained", li)
		}
	}
}

// TestBuildPyramidEmpty covers zero-length and nil input.
func TestBuildPyramidEmpty(t *testing.T) {
	for _, samples := range []Sample{nil, {}} {
		p, err := BuildPyramid(samples)
		if err != nil {
			t.Fatalf("BuildPyramid empty: %v", err)
		}

		if p == nil || len(p.Levels) != 0 || p.NumSamples != 0 || p.TotalBuckets != 0 {
			t.Fatalf("empty pyramid malformed: %+v", p)
		}

		if level := p.LevelForWidth(10); level != nil {
			t.Fatalf("LevelForWidth on empty pyramid = %+v, want nil", level)
		}

		if envelopes := p.Aggregate(10); envelopes != nil {
			t.Fatalf("Aggregate on empty pyramid = %+v, want nil", envelopes)
		}
	}
}

// TestBuildPyramidNonFinite verifies NaN and infinities are excluded from
// envelopes and counted, while adjacent finite values remain intact.
func TestBuildPyramidNonFinite(t *testing.T) {
	samples := Sample{
		math.NaN(),
		math.Inf(1),
		math.Inf(-1),
		5,
		-5,
		math.NaN(),
	}

	p, err := BuildPyramid(samples)
	if err != nil {
		t.Fatalf("BuildPyramid: %v", err)
	}

	if p.NonFinite != 4 {
		t.Fatalf("NonFinite = %d, want 4", p.NonFinite)
	}

	for li, level := range p.Levels {
		for bi, b := range level.Buckets {
			if math.IsNaN(b.Min) || math.IsNaN(b.Max) || math.IsInf(b.Min, 0) || math.IsInf(b.Max, 0) {
				t.Fatalf("level %d bucket %d kept a non-finite envelope: %+v", li, bi, b)
			}
		}

		minV, maxV := envelopeAt(t, level, 0, len(samples))

		if minV != -5 || maxV != 5 {
			t.Fatalf("level %d envelope = [%v,%v], want [-5,5]", li, minV, maxV)
		}
	}

	// An all-non-finite input yields zero envelopes, not NaN.
	allBad := Sample{math.NaN(), math.Inf(1)}

	p2, err := BuildPyramid(allBad)
	if err != nil {
		t.Fatalf("BuildPyramid all-bad: %v", err)
	}

	if p2.NonFinite != 2 {
		t.Fatalf("NonFinite = %d, want 2", p2.NonFinite)
	}

	b := p2.Levels[0].Buckets[0]
	if b.Min != 0 || b.Max != 0 {
		t.Fatalf("all-non-finite bucket = %+v, want zero envelope", b)
	}
}

// TestBuildPyramidMemoryCap forces cap pressure and checks the hard limit,
// including the simulate-long-file case where the finest level must coarsen.
func TestBuildPyramidMemoryCap(t *testing.T) {
	const n = 100_000

	samples := make(Sample, n)
	samples[7] = 99 // spike that coarsening must preserve

	const cap = 64

	p, err := BuildPyramid(samples, WithMaxPyramidBuckets(cap))
	if err != nil {
		t.Fatalf("BuildPyramid: %v", err)
	}

	if p.TotalBuckets > cap {
		t.Fatalf("TotalBuckets = %d exceeds cap %d", p.TotalBuckets, cap)
	}

	if p.Levels[0].BucketSize == 1 {
		t.Fatalf("finest level did not coarsen under cap pressure")
	}

	// Even with coarsening, the spike remains visible on every level.
	for li, level := range p.Levels {
		_, maxV := envelopeAt(t, level, 0, n)

		if maxV != 99 {
			t.Fatalf("level %d lost spike after coarsening, max = %v", li, maxV)
		}
	}

	// Invalid cap is a defined error, not a panic.
	if _, err := BuildPyramid(samples, WithMaxPyramidBuckets(0)); err == nil {
		t.Fatal("expected error for zero cap")
	}

	if _, err := BuildPyramid(samples, WithMaxPyramidBuckets(-3)); err == nil {
		t.Fatal("expected error for negative cap")
	}

	// Cap of one: the whole file becomes a single bucket per level count 1.
	pOne, err := BuildPyramid(samples, WithMaxPyramidBuckets(1))
	if err != nil {
		t.Fatalf("BuildPyramid cap=1: %v", err)
	}

	if len(pOne.Levels) != 1 || len(pOne.Levels[0].Buckets) != 1 {
		t.Fatalf("cap=1 pyramid = %+v, want one level with one bucket", pOne.Levels)
	}
}

// TestLevelForWidth checks level selection across all width regimes.
func TestLevelForWidth(t *testing.T) {
	samples := make(Sample, 1024)

	p, err := BuildPyramid(samples)
	if err != nil {
		t.Fatalf("BuildPyramid: %v", err)
	}

	finest := p.Levels[0]
	coarsest := p.Levels[len(p.Levels)-1]

	if got := p.LevelForWidth(0); got != nil {
		t.Fatalf("width 0 = %+v, want nil", got)
	}

	if got := p.LevelForWidth(-5); got != nil {
		t.Fatalf("negative width = %+v, want nil", got)
	}

	// Width 1: the finest level already has enough buckets, so level 0 wins.
	if got := p.LevelForWidth(1); got != finest {
		t.Fatalf("width 1 selected bucket size %d, want finest %d", got.BucketSize, finest.BucketSize)
	}

	// Width exceeding every level's bucket count: clamp to coarsest.
	if got := p.LevelForWidth(1 << 20); got != coarsest {
		t.Fatalf("huge width selected bucket size %d, want coarsest %d", got.BucketSize, coarsest.BucketSize)
	}

	// Width equal to the finest bucket count: finest level.
	if got := p.LevelForWidth(len(finest.Buckets)); got != finest {
		t.Fatalf("exact-width selected bucket size %d, want finest", got.BucketSize)
	}

	// Width 2: the finest level with at least 2 buckets, never a level with
	// fewer buckets than requested.
	got := p.LevelForWidth(2)
	if len(got.Buckets) < 2 {
		t.Fatalf("width 2 got level with %d buckets", len(got.Buckets))
	}
}

// TestAggregateCoverageAndPositions checks pixel envelopes tile the sample
// range exactly, in order, with stable time coordinates and correct min/max.
func TestAggregatePositions(t *testing.T) {
	const n = 1000

	samples := make(Sample, n)
	for i := range samples {
		samples[i] = float64(i)
	}

	p, err := BuildPyramid(samples)
	if err != nil {
		t.Fatalf("BuildPyramid: %v", err)
	}

	const width = 37 // deliberately does not divide n

	envelopes := p.Aggregate(width)
	if len(envelopes) != width {
		t.Fatalf("got %d envelopes, want %d", len(envelopes), width)
	}

	level := p.LevelForWidth(width)
	bucketCount := len(level.Buckets)

	for i, env := range envelopes {
		first := i * bucketCount / width
		last := (i+1)*bucketCount/width - 1

		wantStart := level.Buckets[first].Start
		wantEnd := level.Buckets[last].End

		if env.Start != wantStart || env.End != wantEnd {
			t.Fatalf("pixel %d range = [%d,%d), want [%d,%d)", i, env.Start, env.End, wantStart, wantEnd)
		}

		// Ramp data: envelope extremes are the first/last sample in range.
		if env.Min != float64(wantStart) || env.Max != float64(wantEnd-1) {
			t.Fatalf("pixel %d envelope = [%v,%v], want [%d,%d]", i, env.Min, env.Max, wantStart, wantEnd-1)
		}

		if i > 0 && env.Start != envelopes[i-1].End {
			t.Fatalf("pixel %d leaves a gap/overlap against pixel %d", i, i-1)
		}
	}

	if envelopes[0].Start != 0 || envelopes[width-1].End != n {
		t.Fatal("envelopes do not cover the full sample range")
	}
}

// TestAggregateWholeBucketAssignment guarantees no level bucket is split
// across two pixels, which is the property that keeps short transients alive:
// pixels group contiguous, whole buckets and cover every bucket exactly once.
func TestAggregateWholeBucketAssignment(t *testing.T) {
	const n = 1024

	samples := make(Sample, n)

	p, err := BuildPyramid(samples)
	if err != nil {
		t.Fatalf("BuildPyramid: %v", err)
	}

	const width = 100

	envelopes := p.Aggregate(width)
	level := p.LevelForWidth(width)
	bucketCount := len(level.Buckets)

	covered := 0

	for i, env := range envelopes {
		first := i * bucketCount / width
		last := (i+1)*bucketCount/width - 1

		if first >= bucketCount {
			break
		}

		if last < first {
			last = first
		}

		covered += last - first + 1

		if env.Start != level.Buckets[first].Start || env.End != level.Buckets[last].End {
			t.Fatalf("pixel %d does not align to whole buckets", i)
		}
	}

	if covered != bucketCount {
		t.Fatalf("pixels covered %d buckets, want %d", covered, bucketCount)
	}
}

// TestAggregateSpikeVisibleAtOverviewWidth is the end-to-end pyramid check:
// a single-sample spike still shows up when rendering into a handful of
// overview pixels.
func TestAggregateSpikeVisibleAtOverviewWidth(t *testing.T) {
	const n = 1 << 16

	samples := make(Sample, n)
	samples[12345] = 77

	p, err := BuildPyramid(samples)
	if err != nil {
		t.Fatalf("BuildPyramid: %v", err)
	}

	for _, width := range []int{1, 2, 10, 100, 1000} {
		envelopes := p.Aggregate(width)

		found := false

		for _, env := range envelopes {
			if env.Max == 77 {
				found = true
				break
			}
		}

		if !found {
			t.Fatalf("spike missing at overview width %d", width)
		}
	}
}

// TestAggregateMorePixelsThanBuckets: when width exceeds even the finest
// bucket count, the level clamps to the coarsest level; the first pixel
// carries the whole-range envelope and the remaining pixels are empty zero
// envelopes that renderers can skip.
func TestAggregateMorePixelsThanBuckets(t *testing.T) {
	const n = 4

	samples := Sample{1, 2, 3, 4}

	p, err := BuildPyramid(samples)
	if err != nil {
		t.Fatalf("BuildPyramid: %v", err)
	}

	const width = 10

	envelopes := p.Aggregate(width)
	if len(envelopes) != width {
		t.Fatalf("got %d envelopes, want %d", len(envelopes), width)
	}

	// Pixel 0 holds the coarsest bucket: every sample, full extremes.
	if envelopes[0].Start != 0 || envelopes[0].End != n || envelopes[0].Min != 1 || envelopes[0].Max != 4 {
		t.Fatalf("pixel 0 wrong envelope: %+v", envelopes[0])
	}

	for i := 1; i < width; i++ {
		if envelopes[i].Start != 0 || envelopes[i].End != 0 || envelopes[i].Min != 0 || envelopes[i].Max != 0 {
			t.Fatalf("extra pixel %d should be an empty zero envelope: %+v", i, envelopes[i])
		}
	}
}

// BenchmarkBuildPyramidLargeFile models a long multi-hour file and shows that
// construction stays linear in the number of samples while respecting the
// configured bucket cap.
func BenchmarkBuildPyramidLargeFile(b *testing.B) {
	const n = 4_000_000 // ~90 seconds of 44.1 kHz mono per channel

	samples := make(Sample, n)

	for i := range samples {
		samples[i] = float64(i%101) - 50
	}

	samples[n/2] = 1e6 // single transient

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		p, err := BuildPyramid(samples, WithMaxPyramidBuckets(100_000))
		if err != nil {
			b.Fatal(err)
		}

		if p.TotalBuckets > 100_000 {
			b.Fatalf("cap exceeded: %d buckets", p.TotalBuckets)
		}
	}
}

// TestBuildStereoPyramidsOrder guarantees left/right ordering and alignment.
func TestBuildStereoPyramidsOrder(t *testing.T) {
	left := Sample{1, 2, 3, 4}
	right := Sample{10, 20, 30, 40}

	lp, rp, err := BuildStereoPyramids(left, right)
	if err != nil {
		t.Fatalf("BuildStereoPyramids: %v", err)
	}

	if got := lp.Levels[0].Buckets[2].Max; got != 3 {
		t.Fatalf("left pyramid bucket value = %v, want 3", got)
	}

	if got := rp.Levels[0].Buckets[2].Max; got != 30 {
		t.Fatalf("right pyramid bucket value = %v, want 30", got)
	}

	if lp.NumSamples != rp.NumSamples {
		t.Fatal("channel pyramids misaligned")
	}
}

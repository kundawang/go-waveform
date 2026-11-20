# go-waveform
<p align="center">
	<img src="image.svg" alt="go-waveform">
</p>

Golang project for the purpose of practicing, understanding golang and audio file format.

## Usage

## Requirements

Go 1.11 (Go Modules)

### Cli

Generate waveform image via CLI

```bash
GO111MODULE=on make
./bin/go-waveform -t dark -r 1000 -f png example/violin.wav

open example/violin-L.png
open example/violin-R.png
```

### Import as package

Import as package,

```go
import "github.com/cettoana/go-waveform"
```

and decode wav audio file

```go
w := waveform.DecodeWav(wavFileBytes)
fmt.Println(w.BitsPerSample)

data, _ := w.GetData()

if stereoData, ok := data.(*waveform.StereoData); ok {
  fmt.Println(stereoData.RSample)
  fmt.Println(stereoData.LSample)
} else if monoData, ok := data.(*waveform.MonoData); ok {
  fmt.Println(monoData.Sample)
}
```

### Waveform pyramid

Naive sampling drops short transients when a whole overview pixel covers many
samples: a one-sample pop can sit between two sampled points and disappear.
`BuildPyramid` builds a multi-resolution min/max hierarchy from the PCM
samples instead. Level 0 is the finest level and each following level doubles
the bucket size, so every extreme is preserved at every zoom level.

```go
pyramid, err := waveform.BuildPyramid(samples)
if err != nil {
  // only fails for a non-positive memory cap
}

// Pick the level suited to an overview of width pixels and re-aggregate it
// into exactly width envelopes, one per pixel.
level := pyramid.LevelForWidth(width)
envelopes := pyramid.Aggregate(width) // []MinMaxBucket with stable [Start, End) sample indices
```

`MinMaxBucket.Start`/`End` are indices into the original PCM slice, so every
level shares the same time coordinates and channel order is unchanged. The
final bucket keeps a short tail when the sample count is not an exact
multiple of the bucket size. Non-finite samples (NaN/Inf) are skipped and
counted in `pyramid.NonFinite`.

Memory is bounded: the total number of buckets across all levels never
exceeds `DefaultMaxPyramidBuckets` (override with
`WithMaxPyramidBuckets`); when an input would exceed it the finest level is
coarsened rather than allocating without bound. Empty input yields an empty,
non-nil pyramid with no levels.

The default image renderer (`image.OutputWaveformImage`) builds and uses the
pyramid internally. Prebuild one yourself and reuse it across renders with
`image.OutputPyramidImage`; the legacy direct line plot remains available
with `Style: "original"`.

### Test

```bash
go test ./...
```

### Todo

- More wav file details
- Converter
- Raw Waveform
- Waveform image customization

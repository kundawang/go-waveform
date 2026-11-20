package image

import (
	"errors"
	"fmt"
	"image/color"
	"math"

	"github.com/cettoana/go-waveform"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
)

// Option image option
type Option struct {
	Fast       bool
	FileName   string
	FileType   string
	Resolution int
	Style      string
	Width      int
	Theme      string
	// MaxPyramidBuckets overrides the per-channel memory cap used when the
	// renderer builds its own pyramid. Zero keeps the library default.
	MaxPyramidBuckets int
}

// OutputWaveformImage output waveform image
func OutputWaveformImage(data interface{}, option *Option) error {
	var outputFn func(waveform.Sample, *waveform.Bound, *Option, string) error

	if option.Style == "original" {
		outputFn = outputOriginalWavefromImage
	} else {
		outputFn = outputWaveformImage
	}

	switch data := data.(type) {
	case *waveform.MonoData:
		return outputFn(data.Sample, data.Bound, option, "")
	case *waveform.StereoData:
		if err := outputFn(data.LSample, data.Bound, option, "-L"); err != nil {
			return err
		}

		return outputFn(data.RSample, data.Bound, option, "-R")
	default:
		return errors.New("sound system unsupport")
	}
}

// OutputPyramidImage renders a prebuilt, reusable pyramid into an image file.
// The pyramid's level hierarchy decides which resolution is drawn: the level
// is chosen by the target pixel width and re-aggregated into one min/max
// envelope per pixel, so single-sample spikes stay visible at every zoom
// level instead of disappearing under naive sampling.
func OutputPyramidImage(pyramid *waveform.Pyramid, bound *waveform.Bound, option *Option, postfix string) error {
	p, err := plot.New()
	if err != nil {
		return err
	}

	widthPx, stroke, n := renderGeometry(option)

	envelopes := pyramid.Aggregate(n)

	g := 155
	d := 1

	for _, envelope := range envelopes {
		// An envelope without a sample range means there are more pixels than
		// buckets at this level; skip it rather than draw a stray line at the
		// origin. Real silence envelopes keep Start < End and are still drawn.
		if envelope.Start == envelope.End {
			continue
		}

		xys := plotter.XYs{
			{X: float64(envelope.Start), Y: envelope.Min},
			{X: float64(envelope.Start), Y: envelope.Max},
		}

		l, err := plotter.NewLine(xys)
		if err != nil {
			return err
		}

		l.LineStyle.Width = vg.Points(stroke)
		l.Color = &color.RGBA{R: 50, G: uint8(g), B: 240, A: 255}

		p.Add(l)

		g += d

		if g > 225 {
			g = 225 - 1
			d = -1
		} else if g < 155 {
			g = 156
			d = 1
		}
	}

	p.HideX()
	p.HideY()
	p.X.Min = 0
	p.X.Max = float64(pyramid.NumSamples)
	p.Y.Min = bound.Lower
	p.Y.Max = bound.Upper
	p.BackgroundColor = getBackgroundColor(option.Theme)

	fileName := fmt.Sprintf("%s%s.%s", option.FileName, postfix, option.FileType)

	return p.Save(vg.Points(float64(widthPx)), vg.Points(540), fileName)
}

// outputOriginalWavefromImage keeps the legacy direct-drawing entry: every
// PCM sample (or one in every m samples in fast mode) is plotted as a line.
func outputOriginalWavefromImage(sample waveform.Sample, bound *waveform.Bound, option *Option, postfix string) error {
	var points plotter.XYs

	p, err := plot.New()
	if err != nil {
		return err
	}

	if option.Fast {
		m := int(math.Ceil(float64(len(sample)) / 100000))
		if m < 1 {
			m = 1
		}

		for i := 0; i < len(sample); i += m {
			points = append(points, plotter.XY{X: float64(i), Y: sample[i]})
		}
	} else {
		for i, v := range sample {
			points = append(points, plotter.XY{X: float64(i), Y: v})
		}
	}

	l, err := plotter.NewLine(points)
	if err != nil {
		return err
	}

	l.LineStyle.Width = vg.Points(1)
	l.LineStyle.Color = color.RGBA{G: 225, B: 255, A: 255}

	p.Add(l)

	p.HideX()
	p.HideY()
	p.X.Min = 0
	p.X.Max = float64(len(sample))
	p.Y.Min = bound.Lower
	p.Y.Max = bound.Upper
	p.BackgroundColor = color.Black

	fileName := fmt.Sprintf("%s%s.%s", option.FileName, postfix, option.FileType)

	return p.Save(vg.Points(5000), vg.Points(540), fileName)
}

// outputWaveformImage builds a min/max pyramid from the PCM samples and draws
// one vertical envelope per output pixel. The legacy flat bucketing dropped
// short transients when many samples shared a pixel; the pyramid keeps every
// min/max extreme at every zoom level.
func outputWaveformImage(sample waveform.Sample, bound *waveform.Bound, option *Option, postfix string) error {
	pyramidOpts := make([]waveform.PyramidOption, 0, 1)

	if option.MaxPyramidBuckets > 0 {
		pyramidOpts = append(pyramidOpts, waveform.WithMaxPyramidBuckets(option.MaxPyramidBuckets))
	}

	pyramid, err := waveform.BuildPyramid(sample, pyramidOpts...)
	if err != nil {
		return err
	}

	return OutputPyramidImage(pyramid, bound, option, postfix)
}

// renderGeometry derives the output width, per-pixel stroke width and the
// number of drawable envelopes from the option.
func renderGeometry(option *Option) (widthPx int, stroke float64, n int) {
	n = option.Resolution
	if n < 1 {
		n = 1
	}

	stroke = float64(2)
	widthPx = n * 5

	if option.Width != 0 {
		widthPx = option.Width
		stroke = float64(widthPx) / float64(n) * 0.4
	}

	return widthPx, stroke, n
}

func getBackgroundColor(theme string) color.Color {
	if theme == "dark" {
		return color.Black
	}

	return color.White
}

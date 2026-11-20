package waveform

import "errors"

// Wav struct
type Wav struct {
	WaveFormat    WaveFormat
	NumChannels   uint16
	SampleRate    uint32
	BitsPerSample uint16
	DataChuckSize uint32

	Data []byte
}

// GetData get wav audio data
func (w *Wav) GetData() (interface{}, error) {
	bytePerSample := int(w.BitsPerSample / 8)
	sampleParser, err := GetSampleParser(w.BitsPerSample, w.WaveFormat)
	if err != nil {
		return nil, err
	}

	if w.NumChannels == 1 {
		sample := parseMonoSample(w.Data, bytePerSample, sampleParser)
		bound, err := GetBound(w.BitsPerSample, w.WaveFormat)
		if err != nil {
			return nil, err
		}

		sample.Bound = bound
		return sample, nil
	}

	if w.NumChannels == 2 {
		sample := parseStereoSample(w.Data, bytePerSample, sampleParser)
		bound, err := GetBound(w.BitsPerSample, w.WaveFormat)
		if err != nil {
			return nil, err
		}

		sample.Bound = bound
		return sample, nil
	}

	return nil, errors.New("failed to sampled data from wav file")
}

// parseMonoSample turns raw data bytes into mono samples. Bytes that do not
// form a complete sample (a truncated wav tail) are ignored rather than
// indexed out of range.
func parseMonoSample(data []byte, bytePerSample int, parser Parser) *MonoData {
	frameCount := len(data) / bytePerSample

	sample := make([]float64, 0, frameCount)

	for i := 0; i < frameCount; i++ {
		start := i * bytePerSample
		s := parser(data[start : start+bytePerSample])

		sample = append(sample, s)
	}

	return &MonoData{
		Sample: sample,
	}
}

// parseStereoSample deinterleaves raw data bytes into left/right samples.
// Frames missing one channel (a truncated wav tail) are dropped, so both
// channels always have the same length and stay time-aligned.
func parseStereoSample(data []byte, bytePerSample int, parser Parser) *StereoData {
	frameSize := bytePerSample * 2
	frameCount := len(data) / frameSize

	lSample := make([]float64, 0, frameCount)
	rSample := make([]float64, 0, frameCount)

	for i := 0; i < frameCount; i++ {
		frameStart := i * frameSize
		l := parser(data[frameStart : frameStart+bytePerSample])
		r := parser(data[frameStart+bytePerSample : frameStart+frameSize])

		lSample = append(lSample, l)
		rSample = append(rSample, r)
	}

	return &StereoData{
		LSample: lSample,
		RSample: rSample,
	}
}

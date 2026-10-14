// Ported from libopus 1.6.1 src/opus_encoder.c (downmix_float,
// downmix_int, downmix_int24, is_digital_silence) and the downmix_func
// type of src/opus_private.h (float build).

package opus

import "github.com/cirocosta/opus/internal/dsp"

// analysisPCM stands for the untyped analysis_pcm pointer (const void *)
// that opus_encode_native passes to run_analysis and to the downmix
// function. At most one field is set; the zero value is NULL. The
// samples are interleaved.
type analysisPCM struct {
	f32 []float32 // float samples, read by downmixFloat
	i16 []int16   // 16-bit samples, read by downmixInt
	i32 []int32   // 24-bit samples, read by downmixInt24
}

// isNil reports whether p stands for a NULL pointer.
func (p analysisPCM) isNil() bool {
	return p.f32 == nil && p.i16 == nil && p.i32 == nil
}

// downmixFunc is downmix_func. It writes subframe samples to y, starting
// at frame offset of x: channel c1, plus channel c2 when c2 > -1, or plus
// all the other channels when c2 == -2. c is the channel count.
type downmixFunc func(x analysisPCM, y []float32, subframe, offset, c1, c2, c int)

// downmixFloat is downmix_float.
func downmixFloat(px analysisPCM, y []float32, subframe, offset, c1, c2, c int) {
	x := px.f32
	y = y[:subframe]
	for j := range y {
		y[j] = float32(x[(j+offset)*c+c1] * dsp.CeltSigScale)
	}
	if c2 > -1 {
		for j := range y {
			y[j] += float32(x[(j+offset)*c+c2] * dsp.CeltSigScale)
		}
	} else if c2 == -2 {
		for ch := 1; ch < c; ch++ {
			for j := range y {
				y[j] += float32(x[(j+offset)*c+ch] * dsp.CeltSigScale)
			}
		}
	}
	// Cap signal to +6 dBFS to avoid problems in the analysis.
	for j := range y {
		if y[j] < -65536 {
			y[j] = -65536
		}
		if y[j] > 65536 {
			y[j] = 65536
		}
		if dsp.CeltIsnan(y[j]) {
			y[j] = 0
		}
	}
}

// downmixInt is downmix_int.
func downmixInt(px analysisPCM, y []float32, subframe, offset, c1, c2, c int) {
	x := px.i16
	y = y[:subframe]
	for j := range y {
		y[j] = float32(x[(j+offset)*c+c1])
	}
	if c2 > -1 {
		for j := range y {
			y[j] += float32(x[(j+offset)*c+c2])
		}
	} else if c2 == -2 {
		for ch := 1; ch < c; ch++ {
			for j := range y {
				y[j] += float32(x[(j+offset)*c+ch])
			}
		}
	}
}

// downmixInt24 is downmix_int24.
func downmixInt24(px analysisPCM, y []float32, subframe, offset, c1, c2, c int) {
	x := px.i32
	y = y[:subframe]
	for j := range y {
		y[j] = float32(float32(x[(j+offset)*c+c1]) * (1.0 / 256))
	}
	if c2 > -1 {
		for j := range y {
			y[j] += float32(float32(x[(j+offset)*c+c2]) * (1.0 / 256))
		}
	} else if c2 == -2 {
		for ch := 1; ch < c; ch++ {
			for j := range y {
				y[j] += float32(float32(x[(j+offset)*c+ch]) * (1.0 / 256))
			}
		}
	}
}

// isDigitalSilence is is_digital_silence: it reports whether no sample of
// the frameSize*channels samples of pcm is above the quantization step of
// a lsbDepth-bit signal.
func isDigitalSilence(pcm []float32, frameSize, channels, lsbDepth int) bool {
	sampleMax := dsp.CeltMaxabsRes(pcm[:frameSize*channels])
	return sampleMax <= float32(1)/float32(int32(1)<<lsbDepth)
}

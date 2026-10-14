// Ported from libopus 1.6.1 silk/resampler_private_IIR_FIR.c.

package silk

import "github.com/cirocosta/opus/internal/kernels"

// interpolationCoefficients stores both halves of each FIR phase contiguously.
// It is initialized once and treated as read-only by all resamplers.
var interpolationCoefficients = func() [12][8]int16 {
	var phases [12][8]int16
	for i := range phases {
		copy(phases[i][:4], resamplerFracFIR12[i][:])
		for j := 0; j < 4; j++ {
			phases[i][4+j] = resamplerFracFIR12[11-i][3-j]
		}
	}
	return phases
}()

// resamplerPrivateIIRFIRInterpol returns the number of output samples written.
func resamplerPrivateIIRFIRInterpol(
	out []int16,
	buf []int16,
	maxIndexQ16, indexIncrementQ16 int32,
) int {
	return kernels.Interpolate(out, buf, &interpolationCoefficients, maxIndexQ16, indexIncrementQ16)
}

// resamplerPrivateIIRFIR is silk_resampler_private_IIR_FIR: upsample
// using a combination of allpass-based 2x upsampling and FIR
// interpolation.
func resamplerPrivateIIRFIR(s *resamplerState, out []int16, in []int16, inLen int) {
	var nSamplesIn int
	buf := s.buf16[:2*s.batchSize+resamplerOrderFIR12]

	// Copy buffered samples to start of buffer.
	copy(buf, s.sFIRi16[:resamplerOrderFIR12])

	// Iterate over blocks of frameSizeIn input samples.
	indexIncrementQ16 := s.invRatioQ16
	for {
		nSamplesIn = minInt(inLen, s.batchSize)

		// Upsample 2x.
		resamplerPrivateUp2HQ(&s.sIIR, buf[resamplerOrderFIR12:], in, nSamplesIn)

		maxIndexQ16 := lshift(int32(nSamplesIn), 16+1) // + 1 because 2x upsampling
		out = out[resamplerPrivateIIRFIRInterpol(out, buf, maxIndexQ16, indexIncrementQ16):]
		in = in[nSamplesIn:]
		inLen -= nSamplesIn

		if inLen > 0 {
			// More iterations to do; copy last part of filtered signal to
			// beginning of buffer.
			copy(buf[:resamplerOrderFIR12], buf[nSamplesIn<<1:(nSamplesIn<<1)+resamplerOrderFIR12])
		} else {
			break
		}
	}

	// Copy last part of filtered signal to the state for the next call.
	copy(s.sFIRi16[:resamplerOrderFIR12], buf[nSamplesIn<<1:(nSamplesIn<<1)+resamplerOrderFIR12])
}

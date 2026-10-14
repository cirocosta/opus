// Ported from libopus 1.6.1 silk/resampler_private_IIR_FIR.c.

package silk

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
	count := int((int64(maxIndexQ16) + int64(indexIncrementQ16) - 1) / int64(indexIncrementQ16))
	out = out[:count]
	var indexQ16 int32
	for n := range out {
		phase := (indexQ16 & 0xffff) * 12 >> 16
		coeff := &interpolationCoefficients[phase]
		i := int(indexQ16 >> 16)
		window := buf[i : i+8]
		resQ15 := int32(window[0]) * int32(coeff[0])
		resQ15 += int32(window[1]) * int32(coeff[1])
		resQ15 += int32(window[2]) * int32(coeff[2])
		resQ15 += int32(window[3]) * int32(coeff[3])
		resQ15 += int32(window[4]) * int32(coeff[4])
		resQ15 += int32(window[5]) * int32(coeff[5])
		resQ15 += int32(window[6]) * int32(coeff[6])
		resQ15 += int32(window[7]) * int32(coeff[7])
		out[n] = int16(sat16(rshiftRound(resQ15, 15)))
		indexQ16 += indexIncrementQ16
	}
	return count
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

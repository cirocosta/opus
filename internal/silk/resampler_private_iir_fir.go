// Ported from libopus 1.6.1 silk/resampler_private_IIR_FIR.c.

package silk

// resamplerPrivateIIRFIRInterpol is silk_resampler_private_IIR_FIR_INTERPOL.
// It returns the number of output samples written.
func resamplerPrivateIIRFIRInterpol(out []int16, buf []int16, maxIndexQ16, indexIncrementQ16 int32) int {
	n := 0
	// Interpolate upsampled signal and store in output array.
	for indexQ16 := int32(0); indexQ16 < maxIndexQ16; indexQ16 += indexIncrementQ16 {
		tableIndex := smulwb(indexQ16&0xFFFF, 12)
		bufPtr := buf[indexQ16>>16:]

		resQ15 := smulbb(int32(bufPtr[0]), int32(resamplerFracFIR12[tableIndex][0]))
		resQ15 = smlabb(resQ15, int32(bufPtr[1]), int32(resamplerFracFIR12[tableIndex][1]))
		resQ15 = smlabb(resQ15, int32(bufPtr[2]), int32(resamplerFracFIR12[tableIndex][2]))
		resQ15 = smlabb(resQ15, int32(bufPtr[3]), int32(resamplerFracFIR12[tableIndex][3]))
		resQ15 = smlabb(resQ15, int32(bufPtr[4]), int32(resamplerFracFIR12[11-tableIndex][3]))
		resQ15 = smlabb(resQ15, int32(bufPtr[5]), int32(resamplerFracFIR12[11-tableIndex][2]))
		resQ15 = smlabb(resQ15, int32(bufPtr[6]), int32(resamplerFracFIR12[11-tableIndex][1]))
		resQ15 = smlabb(resQ15, int32(bufPtr[7]), int32(resamplerFracFIR12[11-tableIndex][0]))
		out[n] = int16(sat16(rshiftRound(resQ15, 15)))
		n++
	}
	return n
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

// Ported from libopus 1.6.1 silk/resampler_private_down_FIR.c.

package silk

// resamplerPrivateDownFIRInterpol is
// silk_resampler_private_down_FIR_INTERPOL. It returns the number of
// output samples written.
func resamplerPrivateDownFIRInterpol(out []int16, buf []int32, firCoefs []int16, firOrder, firFracs int,
	maxIndexQ16, indexIncrementQ16 int32) int {
	n := 0
	switch firOrder {
	case resamplerDownOrderFIR0:
		for indexQ16 := int32(0); indexQ16 < maxIndexQ16; indexQ16 += indexIncrementQ16 {
			// Integer part gives pointer to buffered input.
			bufPtr := buf[indexQ16>>16:]

			// Fractional part gives interpolation coefficients.
			interpolInd := smulwb(indexQ16&0xFFFF, int32(firFracs))

			// Inner product.
			interpolPtr := firCoefs[resamplerDownOrderFIR0/2*int(interpolInd):]
			resQ6 := smulwb(bufPtr[0], int32(interpolPtr[0]))
			resQ6 = smlawb(resQ6, bufPtr[1], int32(interpolPtr[1]))
			resQ6 = smlawb(resQ6, bufPtr[2], int32(interpolPtr[2]))
			resQ6 = smlawb(resQ6, bufPtr[3], int32(interpolPtr[3]))
			resQ6 = smlawb(resQ6, bufPtr[4], int32(interpolPtr[4]))
			resQ6 = smlawb(resQ6, bufPtr[5], int32(interpolPtr[5]))
			resQ6 = smlawb(resQ6, bufPtr[6], int32(interpolPtr[6]))
			resQ6 = smlawb(resQ6, bufPtr[7], int32(interpolPtr[7]))
			resQ6 = smlawb(resQ6, bufPtr[8], int32(interpolPtr[8]))
			interpolPtr = firCoefs[resamplerDownOrderFIR0/2*(firFracs-1-int(interpolInd)):]
			resQ6 = smlawb(resQ6, bufPtr[17], int32(interpolPtr[0]))
			resQ6 = smlawb(resQ6, bufPtr[16], int32(interpolPtr[1]))
			resQ6 = smlawb(resQ6, bufPtr[15], int32(interpolPtr[2]))
			resQ6 = smlawb(resQ6, bufPtr[14], int32(interpolPtr[3]))
			resQ6 = smlawb(resQ6, bufPtr[13], int32(interpolPtr[4]))
			resQ6 = smlawb(resQ6, bufPtr[12], int32(interpolPtr[5]))
			resQ6 = smlawb(resQ6, bufPtr[11], int32(interpolPtr[6]))
			resQ6 = smlawb(resQ6, bufPtr[10], int32(interpolPtr[7]))
			resQ6 = smlawb(resQ6, bufPtr[9], int32(interpolPtr[8]))

			// Scale down, saturate and store in output array.
			out[n] = int16(sat16(rshiftRound(resQ6, 6)))
			n++
		}
	case resamplerDownOrderFIR1:
		for indexQ16 := int32(0); indexQ16 < maxIndexQ16; indexQ16 += indexIncrementQ16 {
			// Integer part gives pointer to buffered input.
			bufPtr := buf[indexQ16>>16:]

			// Inner product.
			resQ6 := smulwb(bufPtr[0]+bufPtr[23], int32(firCoefs[0]))
			resQ6 = smlawb(resQ6, bufPtr[1]+bufPtr[22], int32(firCoefs[1]))
			resQ6 = smlawb(resQ6, bufPtr[2]+bufPtr[21], int32(firCoefs[2]))
			resQ6 = smlawb(resQ6, bufPtr[3]+bufPtr[20], int32(firCoefs[3]))
			resQ6 = smlawb(resQ6, bufPtr[4]+bufPtr[19], int32(firCoefs[4]))
			resQ6 = smlawb(resQ6, bufPtr[5]+bufPtr[18], int32(firCoefs[5]))
			resQ6 = smlawb(resQ6, bufPtr[6]+bufPtr[17], int32(firCoefs[6]))
			resQ6 = smlawb(resQ6, bufPtr[7]+bufPtr[16], int32(firCoefs[7]))
			resQ6 = smlawb(resQ6, bufPtr[8]+bufPtr[15], int32(firCoefs[8]))
			resQ6 = smlawb(resQ6, bufPtr[9]+bufPtr[14], int32(firCoefs[9]))
			resQ6 = smlawb(resQ6, bufPtr[10]+bufPtr[13], int32(firCoefs[10]))
			resQ6 = smlawb(resQ6, bufPtr[11]+bufPtr[12], int32(firCoefs[11]))

			// Scale down, saturate and store in output array.
			out[n] = int16(sat16(rshiftRound(resQ6, 6)))
			n++
		}
	case resamplerDownOrderFIR2:
		for indexQ16 := int32(0); indexQ16 < maxIndexQ16; indexQ16 += indexIncrementQ16 {
			// Integer part gives pointer to buffered input.
			bufPtr := buf[indexQ16>>16:]

			// Inner product.
			resQ6 := smulwb(bufPtr[0]+bufPtr[35], int32(firCoefs[0]))
			resQ6 = smlawb(resQ6, bufPtr[1]+bufPtr[34], int32(firCoefs[1]))
			resQ6 = smlawb(resQ6, bufPtr[2]+bufPtr[33], int32(firCoefs[2]))
			resQ6 = smlawb(resQ6, bufPtr[3]+bufPtr[32], int32(firCoefs[3]))
			resQ6 = smlawb(resQ6, bufPtr[4]+bufPtr[31], int32(firCoefs[4]))
			resQ6 = smlawb(resQ6, bufPtr[5]+bufPtr[30], int32(firCoefs[5]))
			resQ6 = smlawb(resQ6, bufPtr[6]+bufPtr[29], int32(firCoefs[6]))
			resQ6 = smlawb(resQ6, bufPtr[7]+bufPtr[28], int32(firCoefs[7]))
			resQ6 = smlawb(resQ6, bufPtr[8]+bufPtr[27], int32(firCoefs[8]))
			resQ6 = smlawb(resQ6, bufPtr[9]+bufPtr[26], int32(firCoefs[9]))
			resQ6 = smlawb(resQ6, bufPtr[10]+bufPtr[25], int32(firCoefs[10]))
			resQ6 = smlawb(resQ6, bufPtr[11]+bufPtr[24], int32(firCoefs[11]))
			resQ6 = smlawb(resQ6, bufPtr[12]+bufPtr[23], int32(firCoefs[12]))
			resQ6 = smlawb(resQ6, bufPtr[13]+bufPtr[22], int32(firCoefs[13]))
			resQ6 = smlawb(resQ6, bufPtr[14]+bufPtr[21], int32(firCoefs[14]))
			resQ6 = smlawb(resQ6, bufPtr[15]+bufPtr[20], int32(firCoefs[15]))
			resQ6 = smlawb(resQ6, bufPtr[16]+bufPtr[19], int32(firCoefs[16]))
			resQ6 = smlawb(resQ6, bufPtr[17]+bufPtr[18], int32(firCoefs[17]))

			// Scale down, saturate and store in output array.
			out[n] = int16(sat16(rshiftRound(resQ6, 6)))
			n++
		}
	}
	return n
}

// resamplerPrivateDownFIR is silk_resampler_private_down_FIR: resample
// with a 2nd order AR filter followed by FIR interpolation.
func resamplerPrivateDownFIR(s *resamplerState, out []int16, in []int16, inLen int) {
	var nSamplesIn int
	buf := s.buf32[:s.batchSize+s.firOrder]

	// Copy buffered samples to start of buffer.
	copy(buf, s.sFIRi32[:s.firOrder])

	firCoefs := s.coefs[2:]

	// Iterate over blocks of frameSizeIn input samples.
	indexIncrementQ16 := s.invRatioQ16
	for {
		nSamplesIn = minInt(inLen, s.batchSize)

		// Second-order AR filter (output in Q8).
		resamplerPrivateAR2(s.sIIR[:], buf[s.firOrder:], in, s.coefs, nSamplesIn)

		maxIndexQ16 := lshift(int32(nSamplesIn), 16)

		// Interpolate filtered signal.
		out = out[resamplerPrivateDownFIRInterpol(out, buf, firCoefs, s.firOrder,
			s.firFracs, maxIndexQ16, indexIncrementQ16):]

		in = in[nSamplesIn:]
		inLen -= nSamplesIn

		if inLen > 1 {
			// More iterations to do; copy last part of filtered signal to
			// beginning of buffer.
			copy(buf[:s.firOrder], buf[nSamplesIn:nSamplesIn+s.firOrder])
		} else {
			break
		}
	}

	// Copy last part of filtered signal to the state for the next call.
	copy(s.sFIRi32[:s.firOrder], buf[nSamplesIn:nSamplesIn+s.firOrder])
}

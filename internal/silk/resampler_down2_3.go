// Ported from libopus 1.6.1 silk/resampler_down2_3.c.

package silk

const down2_3OrderFIR = 4 // ORDER_FIR

// resamplerDown2_3 is silk_resampler_down2_3: downsample by a factor 2/3,
// low quality. s is the state vector [6].
func resamplerDown2_3(s *[6]int32, out []int16, in []int16, inLen int) {
	var buf [resamplerMaxBatchSizeIn + down2_3OrderFIR]int32
	var nSamplesIn int

	// Copy buffered samples to start of buffer.
	copy(buf[:down2_3OrderFIR], s[:down2_3OrderFIR])

	// Iterate over blocks of frameSizeIn input samples.
	o := 0
	for {
		nSamplesIn = minInt(inLen, resamplerMaxBatchSizeIn)

		// Second-order AR filter (output in Q8).
		resamplerPrivateAR2(
			s[down2_3OrderFIR:],
			buf[down2_3OrderFIR:],
			in,
			resampler2_3COEFSLQ[:],
			nSamplesIn,
		)

		// Interpolate filtered signal.
		p := 0
		counter := nSamplesIn
		for counter > 2 {
			// Inner product.
			resQ6 := smulwb(buf[p+0], int32(resampler2_3COEFSLQ[2]))
			resQ6 = smlawb(resQ6, buf[p+1], int32(resampler2_3COEFSLQ[3]))
			resQ6 = smlawb(resQ6, buf[p+2], int32(resampler2_3COEFSLQ[5]))
			resQ6 = smlawb(resQ6, buf[p+3], int32(resampler2_3COEFSLQ[4]))

			// Scale down, saturate and store in output array.
			out[o] = int16(sat16(rshiftRound(resQ6, 6)))
			o++

			resQ6 = smulwb(buf[p+1], int32(resampler2_3COEFSLQ[4]))
			resQ6 = smlawb(resQ6, buf[p+2], int32(resampler2_3COEFSLQ[5]))
			resQ6 = smlawb(resQ6, buf[p+3], int32(resampler2_3COEFSLQ[3]))
			resQ6 = smlawb(resQ6, buf[p+4], int32(resampler2_3COEFSLQ[2]))

			// Scale down, saturate and store in output array.
			out[o] = int16(sat16(rshiftRound(resQ6, 6)))
			o++

			p += 3
			counter -= 3
		}

		in = in[nSamplesIn:]
		inLen -= nSamplesIn

		if inLen > 0 {
			// More iterations to do; copy last part of filtered signal to
			// beginning of buffer.
			copy(buf[:down2_3OrderFIR], buf[nSamplesIn:nSamplesIn+down2_3OrderFIR])
		} else {
			break
		}
	}

	// Copy last part of filtered signal to the state for the next call.
	copy(s[:down2_3OrderFIR], buf[nSamplesIn:nSamplesIn+down2_3OrderFIR])
}

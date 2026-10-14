// Ported from libopus 1.6.1 silk/resampler_private_up2_HQ.c.

package silk

// resamplerPrivateUp2HQ is silk_resampler_private_up2_HQ: upsample by a
// factor 2, high quality. It uses 2nd order allpass filters for the 2x
// upsampling, followed by a notch filter just above Nyquist. s is the
// resampler state [6]; out holds 2*len samples.
func resamplerPrivateUp2HQ(s *[resamplerMaxIIROrder]int32, out []int16, in []int16, len int) {
	// Internal variables and state are in Q10 format.
	for k := 0; k < len; k++ {
		// Convert to Q10.
		in32 := lshift(int32(in[k]), 10)

		// First all-pass section for even output sample.
		y := in32 - s[0]
		x := smulwb(y, int32(resamplerUp2HQ0[0]))
		out32_1 := s[0] + x
		s[0] = in32 + x

		// Second all-pass section for even output sample.
		y = out32_1 - s[1]
		x = smulwb(y, int32(resamplerUp2HQ0[1]))
		out32_2 := s[1] + x
		s[1] = out32_1 + x

		// Third all-pass section for even output sample.
		y = out32_2 - s[2]
		x = smlawb(y, y, int32(resamplerUp2HQ0[2]))
		out32_1 = s[2] + x
		s[2] = out32_2 + x

		// Apply gain in Q15, convert back to int16 and store to output.
		out[2*k] = int16(sat16(rshiftRound(out32_1, 10)))

		// First all-pass section for odd output sample.
		y = in32 - s[3]
		x = smulwb(y, int32(resamplerUp2HQ1[0]))
		out32_1 = s[3] + x
		s[3] = in32 + x

		// Second all-pass section for odd output sample.
		y = out32_1 - s[4]
		x = smulwb(y, int32(resamplerUp2HQ1[1]))
		out32_2 = s[4] + x
		s[4] = out32_1 + x

		// Third all-pass section for odd output sample.
		y = out32_2 - s[5]
		x = smlawb(y, y, int32(resamplerUp2HQ1[2]))
		out32_1 = s[5] + x
		s[5] = out32_2 + x

		// Apply gain in Q15, convert back to int16 and store to output.
		out[2*k+1] = int16(sat16(rshiftRound(out32_1, 10)))
	}
}

// resamplerPrivateUp2HQWrapper is silk_resampler_private_up2_HQ_wrapper.
func resamplerPrivateUp2HQWrapper(s *resamplerState, out []int16, in []int16, len int) {
	resamplerPrivateUp2HQ(&s.sIIR, out, in, len)
}

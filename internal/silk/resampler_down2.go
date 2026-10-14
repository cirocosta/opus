// Ported from libopus 1.6.1 silk/resampler_down2.c.

package silk

// resamplerDown2 is silk_resampler_down2: downsample by a factor 2.
func resamplerDown2(s *[2]int32, out []int16, in []int16, inLen int) {
	len2 := inLen >> 1

	// Internal variables and state are in Q10 format.
	for k := 0; k < len2; k++ {
		// Convert to Q10.
		in32 := lshift(int32(in[2*k]), 10)

		// All-pass section for even input sample.
		y := in32 - s[0]
		x := smlawb(y, y, resamplerDown2_1)
		out32 := s[0] + x
		s[0] = in32 + x

		// Convert to Q10.
		in32 = lshift(int32(in[2*k+1]), 10)

		// All-pass section for odd input sample, and add to output of
		// previous section.
		y = in32 - s[1]
		x = smulwb(y, resamplerDown2_0)
		out32 = out32 + s[1]
		out32 = out32 + x
		s[1] = in32 + x

		// Add, convert back to int16 and store to output.
		out[k] = int16(sat16(rshiftRound(out32, 11)))
	}
}

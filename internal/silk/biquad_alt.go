// Ported from libopus 1.6.1 silk/biquad_alt.c.

package silk

// biquadAltStride1 is silk_biquad_alt_stride1: second order ARMA filter,
// alternative implementation (direct form II transposed, 2 element
// state vector). out may alias in.
func biquadAltStride1(in []int16, bQ28 *[transitionNB]int32, aQ28 *[transitionNA]int32, s *[2]int32, out []int16, length int) {
	// Negate A_Q28 values and split in two parts.
	a0LQ28 := (-aQ28[0]) & 0x00003FFF // lower part
	a0UQ28 := (-aQ28[0]) >> 14        // upper part
	a1LQ28 := (-aQ28[1]) & 0x00003FFF // lower part
	a1UQ28 := (-aQ28[1]) >> 14        // upper part

	for k := 0; k < length; k++ {
		// S[ 0 ], S[ 1 ]: Q12.
		inval := int32(in[k])
		out32Q14 := lshift(smlawb(s[0], bQ28[0], inval), 2)

		s[0] = s[1] + rshiftRound(smulwb(out32Q14, a0LQ28), 14)
		s[0] = smlawb(s[0], out32Q14, a0UQ28)
		s[0] = smlawb(s[0], bQ28[1], inval)

		s[1] = rshiftRound(smulwb(out32Q14, a1LQ28), 14)
		s[1] = smlawb(s[1], out32Q14, a1UQ28)
		s[1] = smlawb(s[1], bQ28[2], inval)

		// Scale back to Q0 and saturate.
		out[k] = int16(sat16((out32Q14 + (1 << 14) - 1) >> 14))
	}
}

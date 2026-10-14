// Ported from libopus 1.6.1 silk/ana_filt_bank_1.c.

package silk

// Coefficients for 2-band filter bank based on first-order allpass
// filters.
const (
	aFb1_20 = 5394 << 1 // A_fb1_20
	aFb1_21 = -24290    // A_fb1_21: (opus_int16)(20623 << 1)
)

// anaFiltBank1 is silk_ana_filt_bank_1: split signal into two decimated
// bands using first-order allpass filters. outL and outH may alias in.
func anaFiltBank1(in []int16, s *[2]int32, outL, outH []int16, n int) {
	n2 := n >> 1

	// Internal variables and state are in Q10 format.
	for k := 0; k < n2; k++ {
		// Convert to Q10.
		in32 := lshift(int32(in[2*k]), 10)

		// All-pass section for even input sample.
		y := in32 - s[0]
		x := smlawb(y, y, aFb1_21)
		out1 := s[0] + x
		s[0] = in32 + x

		// Convert to Q10.
		in32 = lshift(int32(in[2*k+1]), 10)

		// All-pass section for odd input sample, and add to output of
		// previous section.
		y = in32 - s[1]
		x = smulwb(y, aFb1_20)
		out2 := s[1] + x
		s[1] = in32 + x

		// Add/subtract, convert back to int16 and store to output.
		outL[k] = int16(sat16(rshiftRound(out2+out1, 11)))
		outH[k] = int16(sat16(rshiftRound(out2-out1, 11)))
	}
}

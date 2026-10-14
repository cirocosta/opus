// Ported from libopus 1.6.1 silk/LPC_analysis_filter.c.

package silk

// lpcAnalysisFilter is silk_LPC_analysis_filter (the generic C path,
// USE_CELT_FIR = 0). The state is kept internally and the filter always
// starts with zero state; the first d output samples are set to zero.
// b holds the MA prediction coefficients, Q12 [d].
func lpcAnalysisFilter(out []int16, in []int16, b []int16, len, d int) {
	for ix := d; ix < len; ix++ {
		// in[p-j] is in_ptr[ -j ] in C, with in_ptr = &in[ ix - 1 ].
		p := ix - 1
		out32Q12 := smulbb(int32(in[p]), int32(b[0]))
		// Allowing wrap around so that two wraps can cancel each other.
		// The rare cases where the result wraps around can only be
		// triggered by invalid streams.
		out32Q12 = smlabbOvflw(out32Q12, int32(in[p-1]), int32(b[1]))
		out32Q12 = smlabbOvflw(out32Q12, int32(in[p-2]), int32(b[2]))
		out32Q12 = smlabbOvflw(out32Q12, int32(in[p-3]), int32(b[3]))
		out32Q12 = smlabbOvflw(out32Q12, int32(in[p-4]), int32(b[4]))
		out32Q12 = smlabbOvflw(out32Q12, int32(in[p-5]), int32(b[5]))
		for j := 6; j < d; j += 2 {
			out32Q12 = smlabbOvflw(out32Q12, int32(in[p-j]), int32(b[j]))
			out32Q12 = smlabbOvflw(out32Q12, int32(in[p-j-1]), int32(b[j+1]))
		}

		// Subtract prediction.
		out32Q12 = sub32ovflw(lshift(int32(in[p+1]), 12), out32Q12)

		// Scale to Q0.
		out32 := rshiftRound(out32Q12, 12)

		// Saturate output.
		out[ix] = int16(sat16(out32))
	}

	// Set first d output samples to zero.
	clear(out[:d])
}

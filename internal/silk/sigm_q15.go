// Ported from libopus 1.6.1 silk/sigm_Q15.c.

package silk

// Approximate sigmoid function.

var sigmLUTSlopeQ10 = [6]int32{237, 153, 73, 30, 12, 7}

var sigmLUTPosQ15 = [6]int32{16384, 23955, 28861, 31213, 32178, 32548}

var sigmLUTNegQ15 = [6]int32{16384, 8812, 3906, 1554, 589, 219}

// sigmQ15 is silk_sigm_Q15.
func sigmQ15(inQ5 int) int {
	if inQ5 < 0 {
		// Negative input.
		inQ5 = -inQ5
		if inQ5 >= 6*32 {
			return 0 // Clip
		}
		// Linear interpolation of look up table.
		ind := inQ5 >> 5
		return int(sigmLUTNegQ15[ind] - smulbb(sigmLUTSlopeQ10[ind], int32(inQ5&0x1F)))
	}
	// Positive input.
	if inQ5 >= 6*32 {
		return 32767 // clip
	}
	// Linear interpolation of look up table.
	ind := inQ5 >> 5
	return int(sigmLUTPosQ15[ind] + smulbb(sigmLUTSlopeQ10[ind], int32(inQ5&0x1F)))
}

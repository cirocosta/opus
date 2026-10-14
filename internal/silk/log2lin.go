// Ported from libopus 1.6.1 silk/log2lin.c.

package silk

// log2lin is silk_log2lin: approximation of 2^() (very close inverse of
// silk_lin2log()). Convert input to a linear scale.
func log2lin(inLogQ7 int32) int32 {
	if inLogQ7 < 0 {
		return 0
	} else if inLogQ7 >= 3967 {
		return int32MAX
	}

	out := lshift(1, int(inLogQ7>>7))
	fracQ7 := inLogQ7 & 0x7F
	if inLogQ7 < 2048 {
		// Piece-wise parabolic approximation.
		out = addRshift32(out, out*smlawb(fracQ7, smulbb(fracQ7, 128-fracQ7), -174), 7)
	} else {
		// Piece-wise parabolic approximation.
		out = mla(out, out>>7, smlawb(fracQ7, smulbb(fracQ7, 128-fracQ7), -174))
	}
	return out
}

// Ported from libopus 1.6.1 silk/lin2log.c.

package silk

// lin2log is silk_lin2log: approximation of 128 * log2() (very close
// inverse of silk_log2lin()). Convert input to a log scale.
func lin2log(inLin int32) int32 {
	lz, fracQ7 := clzFrac(inLin)

	// Piece-wise parabolic approximation.
	return addLshift32(smlawb(fracQ7, fracQ7*(128-fracQ7), 179), 31-lz, 7)
}

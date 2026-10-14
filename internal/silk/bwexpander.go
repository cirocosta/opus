// Ported from libopus 1.6.1 silk/bwexpander.c.

package silk

// bwexpander is silk_bwexpander: chirp (bandwidth expand) LP AR filter.
// ar has d coefficients (without the leading 1).
func bwexpander(ar []int16, d int, chirpQ16 int32) {
	chirpMinusOneQ16 := chirpQ16 - 65536

	// NB: Dont use silk_SMULWB, instead of silk_RSHIFT_ROUND( silk_MUL(), 16 ), below.
	// Bias in silk_SMULWB can lead to unstable filters.
	for i := 0; i < d-1; i++ {
		ar[i] = int16(rshiftRound(chirpQ16*int32(ar[i]), 16))
		chirpQ16 += rshiftRound(chirpQ16*chirpMinusOneQ16, 16)
	}
	ar[d-1] = int16(rshiftRound(chirpQ16*int32(ar[d-1]), 16))
}

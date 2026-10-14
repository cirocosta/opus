// Ported from libopus 1.6.1 silk/bwexpander_32.c.

package silk

// bwexpander32 is silk_bwexpander_32: chirp (bandwidth expand) LP AR
// filter, 32-bit coefficients.
func bwexpander32(ar []int32, d int, chirpQ16 int32) {
	chirpMinusOneQ16 := chirpQ16 - 65536

	for i := 0; i < d-1; i++ {
		ar[i] = smulww(chirpQ16, ar[i])
		chirpQ16 += rshiftRound(chirpQ16*chirpMinusOneQ16, 16)
	}
	ar[d-1] = smulww(chirpQ16, ar[d-1])
}

// Ported from libopus 1.6.1 silk/float/bwexpander_FLP.c.

package silk

// bwexpanderFLP is silk_bwexpander_FLP: chirp (bw expand) LP AR filter.
func bwexpanderFLP(ar []float32, d int, chirp float32) {
	cfac := chirp

	for i := 0; i < d-1; i++ {
		ar[i] = float32(ar[i] * cfac)
		cfac = float32(cfac * chirp)
	}
	ar[d-1] = float32(ar[d-1] * cfac)
}

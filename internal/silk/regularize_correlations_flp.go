// Ported from libopus 1.6.1 silk/float/regularize_correlations_FLP.c.

package silk

// regularizeCorrelationsFLP is silk_regularize_correlations_FLP: add
// noise to the matrix diagonal.
func regularizeCorrelationsFLP(xx []float32, x0 []float32, noise float32, d int) {
	for i := 0; i < d; i++ {
		xx[i*d+i] += noise
	}
	x0[0] += noise
}

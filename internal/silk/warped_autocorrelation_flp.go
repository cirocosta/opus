// Ported from libopus 1.6.1 silk/float/warped_autocorrelation_FLP.c.

package silk

// warpedAutocorrelationFLP is silk_warped_autocorrelation_FLP:
// autocorrelations for a warped frequency axis. order must be even.
func warpedAutocorrelationFLP(corr []float32, input []float32, warping float32, length, order int) {
	var (
		state [maxShapeLPCOrder + 1]float64
		c     [maxShapeLPCOrder + 1]float64
	)
	w := float64(warping)

	// Loop over samples.
	for n := 0; n < length; n++ {
		tmp1 := float64(input[n])
		// Loop over allpass sections.
		for i := 0; i < order; i += 2 {
			// Output of allpass section. We voluntarily use two multiples
			// instead of factoring the expression to reduce the length of
			// the dependency chain (tmp1->tmp2->tmp1... ).
			tmp2 := state[i] + float64(w*state[i+1]) - float64(w*tmp1)
			state[i] = tmp1
			c[i] += float64(state[0] * tmp1)
			// Output of allpass section.
			tmp1 = state[i+1] + float64(w*state[i+2]) - float64(w*tmp2)
			state[i+1] = tmp2
			c[i+1] += float64(state[0] * tmp2)
		}
		state[order] = tmp1
		c[order] += float64(state[0] * tmp1)
	}

	// Copy correlations in float output format.
	for i := 0; i < order+1; i++ {
		corr[i] = float32(c[i])
	}
}

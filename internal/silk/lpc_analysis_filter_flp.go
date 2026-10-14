// Ported from libopus 1.6.1 silk/float/LPC_analysis_filter_FLP.c.

package silk

// lpcAnalysisFilterFLP is silk_LPC_analysis_filter_FLP: LPC analysis
// filter. The state is kept internally and the filter always starts
// with zero state; the first order output samples are set to zero.
// order is 6, 8, 10, 12 or 16. The sums keep the left-to-right order of
// the unrolled C filters (silk_LPC_analysis_filterN_FLP).
func lpcAnalysisFilterFLP(rLPC []float32, predCoef []float32, s []float32, length, order int) {
	for ix := order; ix < length; ix++ {
		p := ix - 1

		// Short-term prediction.
		lpcPred := float32(s[p] * predCoef[0])
		for j := 1; j < order; j++ {
			lpcPred += float32(s[p-j] * predCoef[j])
		}

		// Prediction error.
		rLPC[ix] = s[p+1] - lpcPred
	}

	// Set first Order output samples to zero.
	for i := 0; i < order; i++ {
		rLPC[i] = 0
	}
}

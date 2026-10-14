// Ported from libopus 1.6.1 silk/float/LPC_analysis_filter_FLP.c.

package silk

import "github.com/cirocosta/opus/internal/kernels"

// lpcAnalysisFilterFLP is silk_LPC_analysis_filter_FLP: LPC analysis
// filter. It reads the prediction history from s; the first order output
// samples are set to zero. rLPC and s must not overlap.
// order is 6, 8, 10, 12 or 16. The sums keep the left-to-right order of
// the unrolled C filters (silk_LPC_analysis_filterN_FLP).
func lpcAnalysisFilterFLP(rLPC []float32, predCoef []float32, s []float32, length, order int) {
	kernels.LPC(rLPC, s, predCoef[:order], length)
}

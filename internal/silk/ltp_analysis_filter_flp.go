// Ported from libopus 1.6.1 silk/float/LTP_analysis_filter_FLP.c.

package silk

// ltpAnalysisFilterFLP is silk_LTP_analysis_filter_FLP. x[xOff] is the
// C x pointer: the input signal with preLength preceding samples (and
// the pitch lag history before it).
func ltpAnalysisFilterFLP(
	ltpRes []float32,
	x []float32,
	xOff int,
	b *[ltpOrder * maxNbSubfr]float32,
	pitchL *[maxNbSubfr]int,
	invGains *[maxNbSubfr]float32,
	subfrLength, nbSubfr, preLength int,
) {
	var btmp [ltpOrder]float32

	xPtr := xOff
	ltpResPtr := 0
	for k := 0; k < nbSubfr; k++ {
		xLagPtr := xPtr - pitchL[k]
		invGain := invGains[k]
		for i := 0; i < ltpOrder; i++ {
			btmp[i] = b[k*ltpOrder+i]
		}

		// LTP analysis FIR filter.
		for i := 0; i < subfrLength+preLength; i++ {
			r := x[xPtr+i]
			// Subtract long-term prediction.
			for j := 0; j < ltpOrder; j++ {
				r -= float32(btmp[j] * x[xLagPtr+ltpOrder/2-j])
			}
			r = float32(r * invGain)
			ltpRes[ltpResPtr+i] = r
			xLagPtr++
		}

		// Update pointers.
		ltpResPtr += subfrLength + preLength
		xPtr += subfrLength
	}
}

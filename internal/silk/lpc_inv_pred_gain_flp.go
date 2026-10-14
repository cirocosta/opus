// Ported from libopus 1.6.1 silk/float/LPC_inv_pred_gain_FLP.c.

package silk

// lpcInversePredGainFLP is silk_LPC_inverse_pred_gain_FLP: compute the
// inverse of the LPC prediction gain, and test if the LPC coefficients
// are stable (all poles within unit circle). It returns the inverse
// prediction gain, energy domain, or 0 if unstable. The libopus encoder
// does not call it.
func lpcInversePredGainFLP(a []float32, order int) float32 {
	var atmp [silkMaxOrderLPC]float32

	copy(atmp[:order], a[:order])

	invGain := 1.0
	for k := order - 1; k > 0; k-- {
		rc := -float64(atmp[k])
		rcMult1 := float64(float32(1.0)) - float64(rc*rc)
		invGain *= rcMult1
		if float64(invGain*float64(maxPredictionPowerGain)) < 1.0 {
			return 0.0
		}
		rcMult2 := 1.0 / rcMult1
		for n := 0; n < (k+1)>>1; n++ {
			tmp1 := float64(atmp[n])
			tmp2 := float64(atmp[k-n-1])
			atmp[n] = float32((tmp1 - float64(tmp2*rc)) * rcMult2)
			atmp[k-n-1] = float32((tmp2 - float64(tmp1*rc)) * rcMult2)
		}
	}
	rc := -float64(atmp[0])
	rcMult1 := 1.0 - float64(rc*rc)
	invGain *= rcMult1
	if float64(invGain*float64(maxPredictionPowerGain)) < 1.0 {
		return 0.0
	}
	return float32(invGain)
}

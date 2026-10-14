// Ported from libopus 1.6.1 silk/float/schur_FLP.c.

package silk

// schurFLP is silk_schur_FLP. It returns the residual energy.
func schurFLP(reflCoef []float32, autoCorr []float32, order int) float32 {
	var c [silkMaxOrderLPC + 1][2]float64

	// Copy correlations.
	k := 0
	for {
		c[k][0] = float64(autoCorr[k])
		c[k][1] = c[k][0]
		k++
		if k > order {
			break
		}
	}

	for k = 0; k < order; k++ {
		// Get reflection coefficient.
		den := c[0][1]
		if !(den > float64(float32(1e-9))) {
			den = float64(float32(1e-9))
		}
		rcTmp := -c[k+1][0] / den

		// Save the output.
		reflCoef[k] = float32(rcTmp)

		// Update correlations.
		for n := 0; n < order-k; n++ {
			ctmp1 := c[n+k+1][0]
			ctmp2 := c[n][1]
			c[n+k+1][0] = ctmp1 + float64(ctmp2*rcTmp)
			c[n][1] = ctmp2 + float64(ctmp1*rcTmp)
		}
	}

	// Return residual energy.
	return float32(c[0][1])
}

// Ported from libopus 1.6.1 silk/float/burg_modified_FLP.c.

package silk

import "math"

// burgModifiedFLP is silk_burg_modified_FLP: compute reflection
// coefficients from the input signal. x holds nb_subfr subframes of
// subfr_length samples (including D preceding samples). It returns the
// residual energy.
// preserve the reference prediction, gain and numerical decision sequence
//
//revive:disable-next-line:cognitive-complexity
func burgModifiedFLP(
	a []float32,
	x []float32,
	minInvGain float32,
	subfrLength, nbSubfr, d int,
) float32 {
	var (
		cFirstRow [silkMaxOrderLPC]float64
		cLastRow  [silkMaxOrderLPC]float64
		cAf       [silkMaxOrderLPC + 1]float64
		cAb       [silkMaxOrderLPC + 1]float64
		af        [silkMaxOrderLPC]float64
	)

	// Compute autocorrelations, added over subframes.
	c0 := energyFLP(x, nbSubfr*subfrLength)
	for s := 0; s < nbSubfr; s++ {
		xPtr := x[s*subfrLength:]
		for n := 1; n < d+1; n++ {
			cFirstRow[n-1] += innerProductFLP(xPtr, xPtr[n:], subfrLength-n)
		}
	}
	cLastRow = cFirstRow

	// Initialize.
	cAf[0] = c0 + float64(float64(findLPCCondFac)*c0) + float64(float32(1e-9))
	cAb[0] = cAf[0]
	invGain := 1.0
	reachedMaxGain := false
	for n := 0; n < d; n++ {
		// Update first row of correlation matrix (without first element).
		// Update last row of correlation matrix (without last element,
		// stored in reversed order). Update C * Af. Update C * flipud(Af)
		// (stored in reversed order).
		for s := 0; s < nbSubfr; s++ {
			xPtr := x[s*subfrLength:]
			tmp1 := float64(xPtr[n])
			tmp2 := float64(xPtr[subfrLength-n-1])
			for k := 0; k < n; k++ {
				cFirstRow[k] -= float64(float32(xPtr[n] * xPtr[n-k-1]))
				cLastRow[k] -= float64(float32(xPtr[subfrLength-n-1] * xPtr[subfrLength-n+k]))
				atmp := af[k]
				tmp1 += float64(float64(xPtr[n-k-1]) * atmp)
				tmp2 += float64(float64(xPtr[subfrLength-n+k]) * atmp)
			}
			for k := 0; k <= n; k++ {
				cAf[k] -= float64(tmp1 * float64(xPtr[n-k]))
				cAb[k] -= float64(tmp2 * float64(xPtr[subfrLength-n+k-1]))
			}
		}
		tmp1 := cFirstRow[n]
		tmp2 := cLastRow[n]
		for k := 0; k < n; k++ {
			atmp := af[k]
			tmp1 += float64(cLastRow[n-k-1] * atmp)
			tmp2 += float64(cFirstRow[n-k-1] * atmp)
		}
		cAf[n+1] = tmp1
		cAb[n+1] = tmp2

		// Calculate nominator and denominator for the next order
		// reflection (parcor) coefficient.
		num := cAb[n+1]
		nrgB := cAb[0]
		nrgF := cAf[0]
		for k := 0; k < n; k++ {
			atmp := af[k]
			num += float64(cAb[n-k] * atmp)
			nrgB += float64(cAb[k+1] * atmp)
			nrgF += float64(cAf[k+1] * atmp)
		}

		// Calculate the next order reflection (parcor) coefficient.
		rc := float64(-2.0*num) / (nrgF + nrgB)

		// Update inverse prediction gain.
		tmp1 = invGain * (1.0 - float64(rc*rc))
		if tmp1 <= float64(minInvGain) {
			// Max prediction gain exceeded; set reflection coefficient such
			// that max prediction gain is exactly hit.
			rc = math.Sqrt(1.0 - float64(minInvGain)/invGain)
			if num > 0 {
				// Ensure adjusted reflection coefficients has the original
				// sign.
				rc = -rc
			}
			invGain = float64(minInvGain)
			reachedMaxGain = true
		} else {
			invGain = tmp1
		}

		// Update the AR coefficients.
		for k := 0; k < (n+1)>>1; k++ {
			tmp1 = af[k]
			tmp2 = af[n-k-1]
			af[k] = tmp1 + float64(rc*tmp2)
			af[n-k-1] = tmp2 + float64(rc*tmp1)
		}
		af[n] = rc

		if reachedMaxGain {
			// Reached max prediction gain; set remaining coefficients to
			// zero and exit loop.
			for k := n + 1; k < d; k++ {
				af[k] = 0.0
			}
			break
		}

		// Update C * Af and C * Ab.
		for k := 0; k <= n+1; k++ {
			tmp1 = cAf[k]
			cAf[k] += float64(rc * cAb[n-k+1])
			cAb[n-k+1] += float64(rc * tmp1)
		}
	}

	var nrgF float64
	if reachedMaxGain {
		// Convert to float.
		for k := 0; k < d; k++ {
			a[k] = float32(-af[k])
		}
		// Subtract energy of preceding samples from C0.
		for s := 0; s < nbSubfr; s++ {
			c0 -= energyFLP(x[s*subfrLength:], d)
		}
		// Approximate residual energy.
		nrgF = c0 * invGain
	} else {
		// Compute residual energy and store coefficients as float.
		nrgF = cAf[0]
		tmp1 := 1.0
		for k := 0; k < d; k++ {
			atmp := af[k]
			nrgF += float64(cAf[k+1] * atmp)
			tmp1 += float64(atmp * atmp)
			a[k] = float32(-atmp)
		}
		nrgF -= float64(float64(float64(findLPCCondFac)*c0) * tmp1)
	}

	// Return residual energy.
	return float32(nrgF)
}

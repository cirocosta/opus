// Ported from libopus 1.6.1 silk/float/find_LPC_FLP.c.

package silk

import "math"

// findLPCFLP is silk_find_LPC_FLP: LPC analysis.
func (psEncC *encoderState) findLPCFLP(nlsfQ15 []int16, x []float32, minInvGain float32) {
	var (
		a        [maxLPCOrder]float32
		nlsf0Q15 [maxLPCOrder]int16
		aTmp     [maxLPCOrder]float32
		lpcRes   [maxFrameLength + maxNbSubfr*maxLPCOrder]float32
	)

	subfrLength := psEncC.subfrLength + psEncC.predictLPCOrder

	// Default: No interpolation.
	psEncC.indices.nlsfInterpCoefQ2 = 4

	// Burg AR analysis for the full frame.
	resNrg := burgModifiedFLP(
		a[:],
		x,
		minInvGain,
		subfrLength,
		psEncC.nbSubfr,
		psEncC.predictLPCOrder,
	)

	if psEncC.useInterpolatedNLSFs != 0 && psEncC.firstFrameAfterReset == 0 &&
		psEncC.nbSubfr == maxNbSubfr {
		// Optimal solution for last 10 ms; subtract residual energy here,
		// as that's easier than adding it to the residual energy of the
		// first 10 ms in each iteration of the search below.
		resNrg -= burgModifiedFLP(
			aTmp[:],
			x[(maxNbSubfr/2)*subfrLength:],
			minInvGain,
			subfrLength,
			maxNbSubfr/2,
			psEncC.predictLPCOrder,
		)

		// Convert to NLSFs.
		a2nlsfFLP(nlsfQ15, aTmp[:], psEncC.predictLPCOrder)

		// Search over interpolation indices to find the one with lowest
		// residual energy.
		resNrg2nd := float32(math.MaxFloat32)
		for k := 3; k >= 0; k-- {
			// Interpolate NLSFs for first half.
			interpolate(nlsf0Q15[:], psEncC.prevNLSFqQ15[:], nlsfQ15, k, psEncC.predictLPCOrder)

			// Convert to LPC for residual energy evaluation.
			nlsf2aFLP(aTmp[:], nlsf0Q15[:], psEncC.predictLPCOrder)

			// Calculate residual energy with LSF interpolation.
			lpcAnalysisFilterFLP(lpcRes[:], aTmp[:], x, 2*subfrLength, psEncC.predictLPCOrder)
			resNrgInterp := float32(
				energyFLP(lpcRes[psEncC.predictLPCOrder:], subfrLength-psEncC.predictLPCOrder) +
					energyFLP(
						lpcRes[psEncC.predictLPCOrder+subfrLength:],
						subfrLength-psEncC.predictLPCOrder,
					))

			// Determine whether current interpolated NLSFs are best so far.
			if resNrgInterp < resNrg {
				// Interpolation has lower residual energy.
				resNrg = resNrgInterp
				psEncC.indices.nlsfInterpCoefQ2 = int8(k)
			} else if resNrgInterp > resNrg2nd {
				// No reason to continue iterating - residual energies will
				// continue to climb.
				break
			}
			resNrg2nd = resNrgInterp
		}
	}

	if psEncC.indices.nlsfInterpCoefQ2 == 4 {
		// NLSF interpolation is currently inactive, calculate NLSFs from
		// full frame AR coefficients.
		a2nlsfFLP(nlsfQ15, a[:], psEncC.predictLPCOrder)
	}
}

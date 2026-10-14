// Ported from libopus 1.6.1 silk/float/residual_energy_FLP.c.

package silk

const (
	maxIterationsResidualNrg = 10
	regularizationFactor     = float32(1e-8)
)

// residualEnergyCovarFLP is silk_residual_energy_covar_FLP: residual
// energy: nrg = wxx - 2 * wXx * c + c' * wXX * c. The libopus encoder
// does not call it.
// preserve the reference prediction, gain and numerical decision sequence
//
//revive:disable-next-line:cognitive-complexity
func residualEnergyCovarFLP(c []float32, wXX []float32, wXx []float32, wxx float32, d int) float32 {
	var k int
	nrg := float32(0.0)

	regularization := float32(regularizationFactor * (wXX[0] + wXX[d*d-1]))
	for k = 0; k < maxIterationsResidualNrg; k++ {
		nrg = wxx

		tmp := float32(0.0)
		for i := 0; i < d; i++ {
			tmp += float32(wXx[i] * c[i])
		}
		nrg -= float32(2.0 * tmp)

		// Compute c' * wXX * c, assuming wXX is symmetric.
		for i := 0; i < d; i++ {
			tmp = 0.0
			for j := i + 1; j < d; j++ {
				tmp += float32(wXX[i+j*d] * c[j])
			}
			nrg += float32(c[i] * (float32(2.0*tmp) + float32(wXX[i+i*d]*c[i])))
		}
		if nrg > 0 {
			break
		}
		// Add white noise.
		for i := 0; i < d; i++ {
			wXX[i+i*d] += regularization
		}
		// Increase noise for next run.
		regularization *= 2.0
	}
	if k == maxIterationsResidualNrg {
		nrg = 1.0
	}

	return nrg
}

// residualEnergyFLP is silk_residual_energy_FLP: calculates residual
// energies of input subframes where all subframes have LPC_order of
// preceding samples.
func residualEnergyFLP(
	nrgs *[maxNbSubfr]float32,
	x []float32,
	a *[2][maxLPCOrder]float32,
	gains *[maxNbSubfr]float32,
	subfrLength, nbSubfr, lpcOrder int,
) {
	var lpcRes [(maxFrameLength + maxNbSubfr*maxLPCOrder) / 2]float32

	lpcResPtr := lpcOrder
	shift := lpcOrder + subfrLength

	// Filter input to create the LPC residual for each frame half, and
	// measure subframe energies.
	lpcAnalysisFilterFLP(lpcRes[:], a[0][:], x[0*shift:], 2*shift, lpcOrder)
	nrgs[0] = float32(
		float64(float32(gains[0]*gains[0])) * energyFLP(lpcRes[lpcResPtr+0*shift:], subfrLength),
	)
	nrgs[1] = float32(
		float64(float32(gains[1]*gains[1])) * energyFLP(lpcRes[lpcResPtr+1*shift:], subfrLength),
	)

	if nbSubfr == maxNbSubfr {
		lpcAnalysisFilterFLP(lpcRes[:], a[1][:], x[2*shift:], 2*shift, lpcOrder)
		nrgs[2] = float32(
			float64(
				float32(gains[2]*gains[2]),
			) * energyFLP(
				lpcRes[lpcResPtr+0*shift:],
				subfrLength,
			),
		)
		nrgs[3] = float32(
			float64(
				float32(gains[3]*gains[3]),
			) * energyFLP(
				lpcRes[lpcResPtr+1*shift:],
				subfrLength,
			),
		)
	}
}

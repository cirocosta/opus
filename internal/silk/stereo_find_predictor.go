// Ported from libopus 1.6.1 silk/stereo_find_predictor.c.

package silk

// stereoFindPredictor is silk_stereo_find_predictor: find least-squares
// prediction gain for one signal based on another and quantize it. It
// returns the predictor in Q13 and the ratio of residual and mid
// energies in Q14.
func stereoFindPredictor(
	x, y []int16,
	midResAmpQ0 []int32,
	length int,
	smoothCoefQ16 int,
) (predQ13, ratioQ14 int32) {
	// Find predictor.
	nrgx, scale1 := sumSqrShift(x, length)
	nrgy, scale2 := sumSqrShift(y, length)
	scale := maxInt(scale1, scale2)
	scale = scale + (scale & 1) // make even
	nrgy = nrgy >> uint(scale-scale2)
	nrgx = nrgx >> uint(scale-scale1)
	nrgx = int32(maxInt(int(nrgx), 1))
	corr := innerProdAlignedScale(x, y, scale, length)
	predQ13 = div32VarQ(corr, nrgx, 13)
	predQ13 = limit32(predQ13, -(1 << 14), 1<<14)
	pred2Q10 := smulwb(predQ13, predQ13)

	// Faster update for signals with large prediction parameters.
	smoothCoefQ16 = maxInt(smoothCoefQ16, int(abs32(pred2Q10)))

	// Smoothed mid and residual norms.
	scale = scale >> 1
	midResAmpQ0[0] = smlawb(midResAmpQ0[0], lshift(sqrtApprox(nrgx), scale)-midResAmpQ0[0],
		int32(smoothCoefQ16))
	// Residual energy = nrgy - 2 * pred * corr + pred^2 * nrgx.
	nrgy = subLshift32(nrgy, smulwb(corr, predQ13), 3+1)
	nrgy = addLshift32(nrgy, smulwb(nrgx, pred2Q10), 6)
	midResAmpQ0[1] = smlawb(midResAmpQ0[1], lshift(sqrtApprox(nrgy), scale)-midResAmpQ0[1],
		int32(smoothCoefQ16))

	// Ratio of smoothed residual and mid norms.
	ratioQ14 = div32VarQ(midResAmpQ0[1], max32(midResAmpQ0[0], 1), 14)
	ratioQ14 = limit32(ratioQ14, 0, 32767)

	return predQ13, ratioQ14
}

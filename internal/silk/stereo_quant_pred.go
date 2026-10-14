// Ported from libopus 1.6.1 silk/stereo_quant_pred.c.

package silk

// stereoQuantStepQ16 is SILK_FIX_CONST( 0.5 / STEREO_QUANT_SUB_STEPS, 16 ).
var stereoQuantStepQ16 = fixConst(0.5/stereoQuantSubSteps, 16)

// stereoQuantPred is silk_stereo_quant_pred: quantize mid/side
// predictors.
// preserve the reference prediction, gain and numerical decision sequence
//
//revive:disable-next-line:cognitive-complexity
func stereoQuantPred(predQ13 *[2]int32, ix *[2][3]int8) {
	quantPredQ13 := int32(0)

	// Quantize.
	for n := 0; n < 2; n++ {
		// Brute-force search over quantization levels.
		errMinQ13 := int32MAX
	search:
		for i := 0; i < stereoQuantTabSize-1; i++ {
			lowQ13 := int32(stereoPredQuantQ13[i])
			stepQ13 := smulwb(int32(stereoPredQuantQ13[i+1])-lowQ13, stereoQuantStepQ16)
			for j := 0; j < stereoQuantSubSteps; j++ {
				lvlQ13 := smlabb(lowQ13, stepQ13, int32(2*j+1))
				errQ13 := abs32(predQ13[n] - lvlQ13)
				if errQ13 < errMinQ13 {
					errMinQ13 = errQ13
					quantPredQ13 = lvlQ13
					ix[n][0] = int8(i)
					ix[n][1] = int8(j)
				} else {
					// Error increasing, so we're past the optimum.
					break search
				}
			}
		}
		ix[n][2] = int8(div32_16(int32(ix[n][0]), 3))
		ix[n][0] -= ix[n][2] * 3
		predQ13[n] = quantPredQ13
	}

	// Subtract second from first predictor (helps when actually applying
	// these).
	predQ13[0] -= predQ13[1]
}

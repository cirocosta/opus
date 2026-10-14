// Ported from libopus 1.6.1 silk/stereo_LR_to_MS.c.

package silk

// Constants of silk_stereo_LR_to_MS.
var (
	stereoSmoothCoef10MsQ16 = fixConst(stereoRatioSmoothCoef/2, 16) // SILK_FIX_CONST( STEREO_RATIO_SMOOTH_COEF / 2, 16 )
	stereoSmoothCoefQ16     = fixConst(stereoRatioSmoothCoef, 16)   // SILK_FIX_CONST( STEREO_RATIO_SMOOTH_COEF, 16 )
	stereoWidth005Q14       = fixConst(0.05, 14)                    // SILK_FIX_CONST( 0.05, 14 )
	stereoWidth002Q14       = fixConst(0.02, 14)                    // SILK_FIX_CONST( 0.02, 14 )
	stereoWidth095Q14       = fixConst(0.95, 14)                    // SILK_FIX_CONST( 0.95, 14 )
)

// stereoLRToMS is silk_stereo_LR_to_MS: convert a Left/Right stereo
// signal to an adaptive Mid/Side representation. x1buf and x2buf are the
// inputBuf arrays of the two channels: the C x1 and x2 pointers are
// &x1buf[ 2 ] and &x2buf[ 2 ]. The left input becomes the mid signal and
// the right input becomes the side signal. sc holds the scratch buffers.
func stereoLRToMS(state *stereoEncState, x1buf, x2buf []int16, ix *[2][3]int8, midOnlyFlag *int8,
	midSideRatesBps *[2]int32, totalRateBps int32, prevSpeechActQ8, toMono, fsKHz, frameLength int,
	sc *stereoScratch) {
	var predQ13 [2]int32
	side := sc.side[:frameLength+2]
	mid := x1buf // mid = &x1[ -2 ]

	// Convert to basic mid/side signals.
	for n := 0; n < frameLength+2; n++ {
		sum := int32(x1buf[n]) + int32(x2buf[n])
		diff := int32(x1buf[n]) - int32(x2buf[n])
		mid[n] = int16(rshiftRound(sum, 1))
		side[n] = int16(sat16(rshiftRound(diff, 1)))
	}

	// Buffering.
	copy(mid[:2], state.sMid[:])
	copy(side[:2], state.sSide[:])
	copy(state.sMid[:], mid[frameLength:frameLength+2])
	copy(state.sSide[:], side[frameLength:frameLength+2])

	// LP and HP filter mid signal.
	lpMid := sc.lpMid[:frameLength]
	hpMid := sc.hpMid[:frameLength]
	for n := 0; n < frameLength; n++ {
		sum := rshiftRound(addLshift32(int32(mid[n])+int32(mid[n+2]), int32(mid[n+1]), 1), 2)
		lpMid[n] = int16(sum)
		hpMid[n] = int16(int32(mid[n+1]) - sum)
	}

	// LP and HP filter side signal.
	lpSide := sc.lpSide[:frameLength]
	hpSide := sc.hpSide[:frameLength]
	for n := 0; n < frameLength; n++ {
		sum := rshiftRound(addLshift32(int32(side[n])+int32(side[n+2]), int32(side[n+1]), 1), 2)
		lpSide[n] = int16(sum)
		hpSide[n] = int16(int32(side[n+1]) - sum)
	}

	// Find energies and predictors.
	is10msFrame := frameLength == 10*fsKHz
	var smoothCoefQ16 int32
	if is10msFrame {
		smoothCoefQ16 = stereoSmoothCoef10MsQ16
	} else {
		smoothCoefQ16 = stereoSmoothCoefQ16
	}
	smoothCoefQ16 = smulwb(smulbb(int32(prevSpeechActQ8), int32(prevSpeechActQ8)), smoothCoefQ16)

	var lpRatioQ14, hpRatioQ14 int32
	predQ13[0], lpRatioQ14 = stereoFindPredictor(lpMid, lpSide, state.midSideAmpQ0[0:2], frameLength, int(smoothCoefQ16))
	predQ13[1], hpRatioQ14 = stereoFindPredictor(hpMid, hpSide, state.midSideAmpQ0[2:4], frameLength, int(smoothCoefQ16))
	// Ratio of the norms of residual and mid signals.
	fracQ16 := smlabb(hpRatioQ14, lpRatioQ14, 3)
	fracQ16 = min32(fracQ16, 1<<16)

	// Determine bitrate distribution between mid and side, and possibly
	// reduce stereo width.
	if is10msFrame { // Subtract approximate bitrate for coding stereo parameters
		totalRateBps -= 1200
	} else {
		totalRateBps -= 600
	}
	if totalRateBps < 1 {
		totalRateBps = 1
	}
	minMidRateBps := smlabb(2000, int32(fsKHz), 600)
	// Default bitrate distribution: 8 parts for Mid and (5+3*frac) parts
	// for Side. so: mid_rate = ( 8 / ( 13 + 3 * frac ) ) * total_ rate.
	frac3Q16 := mul(3, fracQ16)
	midSideRatesBps[0] = div32VarQ(totalRateBps, (8+5)<<16+frac3Q16, 16+3)
	// If Mid bitrate below minimum, reduce stereo width.
	var widthQ14 int32
	if midSideRatesBps[0] < minMidRateBps {
		midSideRatesBps[0] = minMidRateBps
		midSideRatesBps[1] = totalRateBps - midSideRatesBps[0]
		// width = 4 * ( 2 * side_rate - min_rate ) / ( ( 1 + 3 * frac ) * min_rate ).
		widthQ14 = div32VarQ(lshift(midSideRatesBps[1], 1)-minMidRateBps,
			smulwb(1<<16+frac3Q16, minMidRateBps), 14+2)
		widthQ14 = limit32(widthQ14, 0, 1<<14)
	} else {
		midSideRatesBps[1] = totalRateBps - midSideRatesBps[0]
		widthQ14 = 1 << 14
	}

	// Smoother.
	state.smthWidthQ14 = int16(smlawb(int32(state.smthWidthQ14), widthQ14-int32(state.smthWidthQ14), smoothCoefQ16))

	// At very low bitrates or for inputs that are nearly amplitude
	// panned, switch to panned-mono coding.
	*midOnlyFlag = 0
	if toMono != 0 {
		// Last frame before stereo->mono transition; collapse stereo
		// width.
		widthQ14 = 0
		predQ13[0] = 0
		predQ13[1] = 0
		stereoQuantPred(&predQ13, ix)
	} else if state.widthPrevQ14 == 0 &&
		(8*totalRateBps < 13*minMidRateBps || smulwb(fracQ16, int32(state.smthWidthQ14)) < stereoWidth005Q14) {
		// Code as panned-mono; previous frame already had zero width.
		// Scale down and quantize predictors.
		predQ13[0] = smulbb(int32(state.smthWidthQ14), predQ13[0]) >> 14
		predQ13[1] = smulbb(int32(state.smthWidthQ14), predQ13[1]) >> 14
		stereoQuantPred(&predQ13, ix)
		// Collapse stereo width.
		widthQ14 = 0
		predQ13[0] = 0
		predQ13[1] = 0
		midSideRatesBps[0] = totalRateBps
		midSideRatesBps[1] = 0
		*midOnlyFlag = 1
	} else if state.widthPrevQ14 != 0 &&
		(8*totalRateBps < 11*minMidRateBps || smulwb(fracQ16, int32(state.smthWidthQ14)) < stereoWidth002Q14) {
		// Transition to zero-width stereo.
		// Scale down and quantize predictors.
		predQ13[0] = smulbb(int32(state.smthWidthQ14), predQ13[0]) >> 14
		predQ13[1] = smulbb(int32(state.smthWidthQ14), predQ13[1]) >> 14
		stereoQuantPred(&predQ13, ix)
		// Collapse stereo width.
		widthQ14 = 0
		predQ13[0] = 0
		predQ13[1] = 0
	} else if int32(state.smthWidthQ14) > stereoWidth095Q14 {
		// Full-width stereo coding.
		stereoQuantPred(&predQ13, ix)
		widthQ14 = 1 << 14
	} else {
		// Reduced-width stereo coding; scale down and quantize
		// predictors.
		predQ13[0] = smulbb(int32(state.smthWidthQ14), predQ13[0]) >> 14
		predQ13[1] = smulbb(int32(state.smthWidthQ14), predQ13[1]) >> 14
		stereoQuantPred(&predQ13, ix)
		widthQ14 = int32(state.smthWidthQ14)
	}

	// Make sure to keep on encoding until the tapered output has been
	// transmitted.
	if *midOnlyFlag == 1 {
		state.silentSideLen += int16(frameLength - stereoInterpLenMs*fsKHz)
		if int(state.silentSideLen) < laShapeMs*fsKHz {
			*midOnlyFlag = 0
		} else {
			// Limit to avoid wrapping around.
			state.silentSideLen = 10000
		}
	} else {
		state.silentSideLen = 0
	}

	if *midOnlyFlag == 0 && midSideRatesBps[1] < 1 {
		midSideRatesBps[1] = 1
		midSideRatesBps[0] = int32(maxInt(1, int(totalRateBps-midSideRatesBps[1])))
	}

	// Interpolate predictors and subtract prediction from side channel.
	pred0Q13 := -int32(state.predPrevQ13[0])
	pred1Q13 := -int32(state.predPrevQ13[1])
	wQ24 := lshift(int32(state.widthPrevQ14), 10)
	denomQ16 := div32_16(int32(1)<<16, int32(stereoInterpLenMs*fsKHz))
	delta0Q13 := -rshiftRound(smulbb(predQ13[0]-int32(state.predPrevQ13[0]), denomQ16), 16)
	delta1Q13 := -rshiftRound(smulbb(predQ13[1]-int32(state.predPrevQ13[1]), denomQ16), 16)
	deltawQ24 := lshift(smulwb(widthQ14-int32(state.widthPrevQ14), denomQ16), 10)
	for n := 0; n < stereoInterpLenMs*fsKHz; n++ {
		pred0Q13 += delta0Q13
		pred1Q13 += delta1Q13
		wQ24 += deltawQ24
		sum := lshift(addLshift32(int32(mid[n])+int32(mid[n+2]), int32(mid[n+1]), 1), 9) // Q11
		sum = smlawb(smulwb(wQ24, int32(side[n+1])), sum, pred0Q13)                      // Q8
		sum = smlawb(sum, lshift(int32(mid[n+1]), 11), pred1Q13)                         // Q8
		x2buf[n+1] = int16(sat16(rshiftRound(sum, 8)))
	}

	pred0Q13 = -predQ13[0]
	pred1Q13 = -predQ13[1]
	wQ24 = lshift(widthQ14, 10)
	for n := stereoInterpLenMs * fsKHz; n < frameLength; n++ {
		sum := lshift(addLshift32(int32(mid[n])+int32(mid[n+2]), int32(mid[n+1]), 1), 9) // Q11
		sum = smlawb(smulwb(wQ24, int32(side[n+1])), sum, pred0Q13)                      // Q8
		sum = smlawb(sum, lshift(int32(mid[n+1]), 11), pred1Q13)                         // Q8
		x2buf[n+1] = int16(sat16(rshiftRound(sum, 8)))
	}
	state.predPrevQ13[0] = int16(predQ13[0])
	state.predPrevQ13[1] = int16(predQ13[1])
	state.widthPrevQ14 = int16(widthQ14)
}

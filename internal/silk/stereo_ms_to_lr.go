// Ported from libopus 1.6.1 silk/stereo_MS_to_LR.c.

package silk

// stereoMSToLR is silk_stereo_MS_to_LR: convert adaptive Mid/Side
// representation to Left/Right stereo signal. x1 and x2 hold
// frameLength+2 samples; the first two are the buffered samples.
func stereoMSToLR(
	state *stereoDecState,
	x1, x2 []int16,
	predQ13 *[2]int32,
	fsKHz, frameLength int,
) {
	// Buffering.
	copy(x1[:2], state.sMid[:])
	copy(x2[:2], state.sSide[:])
	copy(state.sMid[:], x1[frameLength:frameLength+2])
	copy(state.sSide[:], x2[frameLength:frameLength+2])

	// Interpolate predictors and add prediction to side channel.
	pred0Q13 := int32(state.predPrevQ13[0])
	pred1Q13 := int32(state.predPrevQ13[1])
	denomQ16 := div32_16(int32(1)<<16, int32(stereoInterpLenMs*fsKHz))
	delta0Q13 := rshiftRound(smulbb(predQ13[0]-int32(state.predPrevQ13[0]), denomQ16), 16)
	delta1Q13 := rshiftRound(smulbb(predQ13[1]-int32(state.predPrevQ13[1]), denomQ16), 16)
	n := 0
	for ; n < stereoInterpLenMs*fsKHz; n++ {
		pred0Q13 += delta0Q13
		pred1Q13 += delta1Q13
		sum := lshift(addLshift32(int32(x1[n])+int32(x1[n+2]), int32(x1[n+1]), 1), 9) // Q11
		sum = smlawb(lshift(int32(x2[n+1]), 8), sum, pred0Q13)                        // Q8
		sum = smlawb(sum, lshift(int32(x1[n+1]), 11), pred1Q13)                       // Q8
		x2[n+1] = int16(sat16(rshiftRound(sum, 8)))
	}
	pred0Q13 = predQ13[0]
	pred1Q13 = predQ13[1]
	for n = stereoInterpLenMs * fsKHz; n < frameLength; n++ {
		sum := lshift(addLshift32(int32(x1[n])+int32(x1[n+2]), int32(x1[n+1]), 1), 9) // Q11
		sum = smlawb(lshift(int32(x2[n+1]), 8), sum, pred0Q13)                        // Q8
		sum = smlawb(sum, lshift(int32(x1[n+1]), 11), pred1Q13)                       // Q8
		x2[n+1] = int16(sat16(rshiftRound(sum, 8)))
	}
	state.predPrevQ13[0] = int16(predQ13[0])
	state.predPrevQ13[1] = int16(predQ13[1])

	// Convert to left/right signals.
	for n = 0; n < frameLength; n++ {
		sum := int32(x1[n+1]) + int32(x2[n+1])
		diff := int32(x1[n+1]) - int32(x2[n+1])
		x1[n+1] = int16(sat16(sum))
		x2[n+1] = int16(sat16(diff))
	}
}

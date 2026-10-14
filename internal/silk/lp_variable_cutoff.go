// Ported from libopus 1.6.1 silk/LP_variable_cutoff.c.

package silk

// Elliptic/Cauer filters designed with 0.1 dB passband ripple, 80 dB
// minimum stopband attenuation, and [0.95 : 0.15 : 0.35] normalized cut
// off frequencies.

// lpInterpolateFilterTaps is silk_LP_interpolate_filter_taps: helper
// function, interpolates the filter taps.
// preserve the reference prediction, gain and numerical decision sequence
//
//revive:disable-next-line:cognitive-complexity
func lpInterpolateFilterTaps(
	bQ28 *[transitionNB]int32,
	aQ28 *[transitionNA]int32,
	ind int,
	facQ16 int32,
) {
	if ind < transitionIntNum-1 {
		if facQ16 > 0 {
			if facQ16 < 32768 { // fac_Q16 is in range of a 16-bit int
				// Piece-wise linear interpolation of B and A.
				for nb := 0; nb < transitionNB; nb++ {
					bQ28[nb] = smlawb(transitionLPBQ28[ind][nb],
						transitionLPBQ28[ind+1][nb]-transitionLPBQ28[ind][nb], facQ16)
				}
				for na := 0; na < transitionNA; na++ {
					aQ28[na] = smlawb(transitionLPAQ28[ind][na],
						transitionLPAQ28[ind+1][na]-transitionLPAQ28[ind][na], facQ16)
				}
			} else { // ( fac_Q16 - ( 1 << 16 ) ) is in range of a 16-bit int
				// Piece-wise linear interpolation of B and A.
				for nb := 0; nb < transitionNB; nb++ {
					bQ28[nb] = smlawb(transitionLPBQ28[ind+1][nb],
						transitionLPBQ28[ind+1][nb]-transitionLPBQ28[ind][nb], facQ16-(1<<16))
				}
				for na := 0; na < transitionNA; na++ {
					aQ28[na] = smlawb(transitionLPAQ28[ind+1][na],
						transitionLPAQ28[ind+1][na]-transitionLPAQ28[ind][na], facQ16-(1<<16))
				}
			}
		} else {
			*bQ28 = transitionLPBQ28[ind]
			*aQ28 = transitionLPAQ28[ind]
		}
	} else {
		*bQ28 = transitionLPBQ28[transitionIntNum-1]
		*aQ28 = transitionLPAQ28[transitionIntNum-1]
	}
}

// lpVariableCutoff is silk_LP_variable_cutoff: low-pass filter with
// variable cutoff frequency based on piece-wise linear interpolation
// between elliptic filters. Start by setting psLP.mode <> 0; deactivate
// by setting psLP.mode = 0.
func lpVariableCutoff(psLP *lpState, frame []int16, frameLength int) {
	var bQ28 [transitionNB]int32
	var aQ28 [transitionNA]int32

	// Run filter if needed.
	if psLP.mode != 0 {
		// Calculate index and interpolation factor for interpolation
		// (TRANSITION_INT_STEPS == 64).
		facQ16 := lshift(transitionFrames-psLP.transitionFrameNo, 16-6)
		ind := int(facQ16 >> 16)
		facQ16 -= lshift(int32(ind), 16)

		// Interpolate filter coefficients.
		lpInterpolateFilterTaps(&bQ28, &aQ28, ind, facQ16)

		// Update transition frame number for next frame.
		psLP.transitionFrameNo = limit32(
			psLP.transitionFrameNo+int32(psLP.mode),
			0,
			transitionFrames,
		)

		// ARMA low-pass filtering.
		biquadAltStride1(frame, &bQ28, &aQ28, &psLP.inLPState, frame, frameLength)
	}
}

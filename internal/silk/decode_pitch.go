// Ported from libopus 1.6.1 silk/decode_pitch.c.

package silk

// decodePitch is silk_decode_pitch: pitch lags from the lag index and
// contour index.
// preserve the reference prediction, gain and numerical decision sequence
//
//revive:disable-next-line:cognitive-complexity
func decodePitch(lagIndex int16, contourIndex int8, pitchLags []int, fsKHz, nbSubfr int) {
	// lagCB returns matrix_ptr( Lag_CB_ptr, k, contourIndex, cbk_size ).
	var lagCB func(k int) int8
	if fsKHz == 8 {
		if nbSubfr == peMaxNbSubfr {
			lagCB = func(k int) int8 { return cbLagsStage2[k][contourIndex] }
		} else {
			lagCB = func(k int) int8 { return cbLagsStage2_10Ms[k][contourIndex] }
		}
	} else {
		if nbSubfr == peMaxNbSubfr {
			lagCB = func(k int) int8 { return cbLagsStage3[k][contourIndex] }
		} else {
			lagCB = func(k int) int8 { return cbLagsStage3_10Ms[k][contourIndex] }
		}
	}

	minLag := int(smulbb(peMinLagMs, int32(fsKHz)))
	maxLag := int(smulbb(peMaxLagMs, int32(fsKHz)))
	lag := minLag + int(lagIndex)

	for k := 0; k < nbSubfr; k++ {
		pitchLags[k] = lag + int(lagCB(k))
		pitchLags[k] = limitInt(pitchLags[k], minLag, maxLag)
	}
}

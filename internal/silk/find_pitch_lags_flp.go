// Ported from libopus 1.6.1 silk/float/find_pitch_lags_FLP.c.

package silk

// findPitchLagsFLP is silk_find_pitch_lags_FLP. x[xOff] is the C x
// pointer (start of the frame, with ltp_mem_length samples of history
// before it). res receives the residual.
func (psEnc *encoderStateFLP) findPitchLagsFLP(psEncCtrl *encoderControlFLP, res []float32, x []float32, xOff int) {
	var (
		autoCorr [maxFindPitchLPCOrd + 1]float32
		a        [maxFindPitchLPCOrd]float32
		reflCoef [maxFindPitchLPCOrd]float32
		wsig     [findPitchLPCWinMax]float32
	)

	// Set up buffer lengths etc based on Fs.
	bufLen := psEnc.sCmn.laPitch + psEnc.sCmn.frameLength + psEnc.sCmn.ltpMemLength

	xBuf := xOff - psEnc.sCmn.ltpMemLength

	// Estimate LPC AR coefficients.

	// Calculate windowed signal.

	// First LA_LTP samples.
	xBufPtr := xBuf + bufLen - psEnc.sCmn.pitchLPCWinLength
	wsigPtr := 0
	applySineWindowFLP(wsig[wsigPtr:], x[xBufPtr:], 1, psEnc.sCmn.laPitch)

	// Middle non-windowed samples.
	wsigPtr += psEnc.sCmn.laPitch
	xBufPtr += psEnc.sCmn.laPitch
	n := psEnc.sCmn.pitchLPCWinLength - (psEnc.sCmn.laPitch << 1)
	copy(wsig[wsigPtr:wsigPtr+n], x[xBufPtr:xBufPtr+n])

	// Last LA_LTP samples.
	wsigPtr += n
	xBufPtr += n
	applySineWindowFLP(wsig[wsigPtr:], x[xBufPtr:], 2, psEnc.sCmn.laPitch)

	// Calculate autocorrelation sequence.
	autocorrelationFLP(autoCorr[:], wsig[:], psEnc.sCmn.pitchLPCWinLength, psEnc.sCmn.pitchEstimationLPCOrder+1)

	// Add white noise, as a fraction of the energy.
	autoCorr[0] += float32(autoCorr[0]*findPitchWhiteNoiseFraction) + 1

	// Calculate the reflection coefficients using Schur.
	resNrg := schurFLP(reflCoef[:], autoCorr[:], psEnc.sCmn.pitchEstimationLPCOrder)

	// Prediction gain.
	psEncCtrl.predGain = autoCorr[0] / maxFloat(resNrg, 1.0)

	// Convert reflection coefficients to prediction coefficients.
	k2aFLP(a[:], reflCoef[:], psEnc.sCmn.pitchEstimationLPCOrder)

	// Bandwidth expansion.
	bwexpanderFLP(a[:], psEnc.sCmn.pitchEstimationLPCOrder, findPitchBandwidthExpansion)

	// LPC analysis filtering.
	lpcAnalysisFilterFLP(res, a[:], x[xBuf:], bufLen, psEnc.sCmn.pitchEstimationLPCOrder)

	if psEnc.sCmn.indices.signalType != typeNoVoiceActivity && psEnc.sCmn.firstFrameAfterReset == 0 {
		// Threshold for pitch estimator.
		thrhld := float32(0.6)
		thrhld -= float32(0.004 * float32(psEnc.sCmn.pitchEstimationLPCOrder))
		thrhld -= float32(float32(0.1*float32(psEnc.sCmn.speechActivityQ8)) * (1.0 / 256.0))
		thrhld -= float32(0.15 * float32(psEnc.sCmn.prevSignalType>>1))
		thrhld -= float32(float32(0.1*float32(psEnc.sCmn.inputTiltQ15)) * (1.0 / 32768.0))

		// Call Pitch estimator.
		if pitchAnalysisCoreFLP(res, &psEncCtrl.pitchL, &psEnc.sCmn.indices.lagIndex,
			&psEnc.sCmn.indices.contourIndex, &psEnc.ltpCorr, psEnc.sCmn.prevLag,
			float32(psEnc.sCmn.pitchEstimationThresholdQ16)/65536.0,
			thrhld, psEnc.sCmn.fsKHz, psEnc.sCmn.pitchEstimationComplexity, psEnc.sCmn.nbSubfr) == 0 {
			psEnc.sCmn.indices.signalType = typeVoiced
		} else {
			psEnc.sCmn.indices.signalType = typeUnvoiced
		}
	} else {
		psEncCtrl.pitchL = [maxNbSubfr]int{}
		psEnc.sCmn.indices.lagIndex = 0
		psEnc.sCmn.indices.contourIndex = 0
		psEnc.ltpCorr = 0
	}
}

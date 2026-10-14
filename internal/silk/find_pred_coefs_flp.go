// Ported from libopus 1.6.1 silk/float/find_pred_coefs_FLP.c.

package silk

import "math"

// findPredCoefsFLP is silk_find_pred_coefs_FLP: find LPC and LTP
// coefficients. resPitch[resOff] and x[xOff] are the C res_pitch and x
// pointers (start of the frame, with history before them).
func (psEnc *encoderStateFLP) findPredCoefsFLP(
	psEncCtrl *encoderControlFLP,
	resPitch []float32,
	resOff int,
	x []float32,
	xOff int,
	condCoding int,
) {
	var (
		xxLTP    [maxNbSubfr * ltpOrder * ltpOrder]float32
		xXLTP    [maxNbSubfr * ltpOrder]float32
		invGains [maxNbSubfr]float32
		// Set to NLSF_Q15 to zero so we don't copy junk to the state.
		nlsfQ15  [maxLPCOrder]int16
		lpcInPre [maxNbSubfr*maxLPCOrder + maxFrameLength]float32
	)

	// Weighting for weighted least squares.
	for i := 0; i < psEnc.sCmn.nbSubfr; i++ {
		invGains[i] = 1.0 / psEncCtrl.gains[i]
	}

	if psEnc.sCmn.indices.signalType == typeVoiced {
		// VOICED.

		// LTP analysis.
		findLTPFLP(
			&xxLTP,
			&xXLTP,
			resPitch,
			resOff,
			&psEncCtrl.pitchL,
			psEnc.sCmn.subfrLength,
			psEnc.sCmn.nbSubfr,
		)

		// Quantize LTP gain parameters.
		quantLTPGainsFLP(
			&psEncCtrl.ltpCoef,
			psEnc.sCmn.indices.ltpIndex[:],
			&psEnc.sCmn.indices.perIndex,
			&psEnc.sCmn.sumLogGainQ7,
			&psEncCtrl.ltpredCodGain,
			&xxLTP,
			&xXLTP,
			psEnc.sCmn.subfrLength,
			psEnc.sCmn.nbSubfr,
		)

		// Control LTP scaling.
		psEnc.ltpScaleCtrlFLP(psEncCtrl, condCoding)

		// Create LTP residual.
		ltpAnalysisFilterFLP(
			lpcInPre[:],
			x,
			xOff-psEnc.sCmn.predictLPCOrder,
			&psEncCtrl.ltpCoef,
			&psEncCtrl.pitchL,
			&invGains,
			psEnc.sCmn.subfrLength,
			psEnc.sCmn.nbSubfr,
			psEnc.sCmn.predictLPCOrder,
		)
	} else {
		// UNVOICED.
		// Create signal with prepended subframes, scaled by inverse gains.
		xPtr := xOff - psEnc.sCmn.predictLPCOrder
		xPrePtr := 0
		for i := 0; i < psEnc.sCmn.nbSubfr; i++ {
			scaleCopyVectorFLP(lpcInPre[xPrePtr:], x[xPtr:], invGains[i],
				psEnc.sCmn.subfrLength+psEnc.sCmn.predictLPCOrder)
			xPrePtr += psEnc.sCmn.subfrLength + psEnc.sCmn.predictLPCOrder
			xPtr += psEnc.sCmn.subfrLength
		}
		for i := 0; i < psEnc.sCmn.nbSubfr*ltpOrder; i++ {
			psEncCtrl.ltpCoef[i] = 0
		}
		psEncCtrl.ltpredCodGain = 0.0
		psEnc.sCmn.sumLogGainQ7 = 0
	}

	// Limit on total predictive coding gain.
	var minInvGain float32
	if psEnc.sCmn.firstFrameAfterReset != 0 {
		minInvGain = 1.0 / maxPredictionPowerGainAfterReset
	} else {
		minInvGain = float32(
			math.Pow(2, float64(psEncCtrl.ltpredCodGain/3)),
		) / maxPredictionPowerGain
		minInvGain /= 0.25 + float32(0.75*psEncCtrl.codingQuality)
	}

	// LPC_in_pre contains the LTP-filtered input for voiced, and the
	// unfiltered input for unvoiced.
	psEnc.sCmn.findLPCFLP(nlsfQ15[:], lpcInPre[:], minInvGain)

	// Quantize LSFs.
	psEnc.sCmn.processNLSFsFLP(&psEncCtrl.predCoef, nlsfQ15[:], psEnc.sCmn.prevNLSFqQ15[:])

	// Calculate residual energy using quantized LPC coefficients.
	residualEnergyFLP(&psEncCtrl.resNrg, lpcInPre[:], &psEncCtrl.predCoef, &psEncCtrl.gains,
		psEnc.sCmn.subfrLength, psEnc.sCmn.nbSubfr, psEnc.sCmn.predictLPCOrder)

	// Copy to prediction struct for use in next frame for interpolation.
	psEnc.sCmn.prevNLSFqQ15 = nlsfQ15
}

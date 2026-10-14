// Ported from libopus 1.6.1 silk/process_NLSFs.c.

package silk

// Constants of silk_process_NLSFs.
var (
	nlsfMu003Q20  = fixConst(0.003, 20)  // SILK_FIX_CONST( 0.003, 20 )
	nlsfMuM001Q28 = fixConst(-0.001, 28) // SILK_FIX_CONST( -0.001, 28 )
)

// processNLSFs is silk_process_NLSFs: limit, stabilize, convert and
// quantize NLSFs.
func (psEncC *encoderState) processNLSFs(predCoefQ12 *[2][maxLPCOrder]int16, pNLSFQ15 []int16, prevNLSFqQ15 []int16) {
	var (
		pNLSF0TempQ15 [maxLPCOrder]int16
		pNLSFWQW      [maxLPCOrder]int16
		pNLSFW0TempQW [maxLPCOrder]int16
	)

	// Calculate mu values.
	// NLSF_mu  = 0.003 - 0.0015 * psEnc->speech_activity.
	nlsfMuQ20 := int(smlawb(nlsfMu003Q20, nlsfMuM001Q28, int32(psEncC.speechActivityQ8)))
	if psEncC.nbSubfr == 2 {
		// Multiply by 1.5 for 10 ms packets.
		nlsfMuQ20 = nlsfMuQ20 + (nlsfMuQ20 >> 1)
	}

	// Calculate NLSF weights.
	nlsfVQWeightsLaroia(pNLSFWQW[:], pNLSFQ15, psEncC.predictLPCOrder)

	// Update NLSF weights for interpolated NLSFs.
	doInterpolate := psEncC.useInterpolatedNLSFs == 1 && psEncC.indices.nlsfInterpCoefQ2 < 4
	if doInterpolate {
		// Calculate the interpolated NLSF vector for the first half.
		interpolate(pNLSF0TempQ15[:], prevNLSFqQ15, pNLSFQ15, int(psEncC.indices.nlsfInterpCoefQ2), psEncC.predictLPCOrder)

		// Calculate first half NLSF weights for the interpolated NLSFs.
		nlsfVQWeightsLaroia(pNLSFW0TempQW[:], pNLSF0TempQ15[:], psEncC.predictLPCOrder)

		// Update NLSF weights with contribution from first half.
		iSqrQ15 := int16(lshift(smulbb(int32(psEncC.indices.nlsfInterpCoefQ2), int32(psEncC.indices.nlsfInterpCoefQ2)), 11))
		for i := 0; i < psEncC.predictLPCOrder; i++ {
			pNLSFWQW[i] = int16((int32(pNLSFWQW[i]) >> 1) + (smulbb(int32(pNLSFW0TempQW[i]), int32(iSqrQ15)) >> 16))
		}
	}

	nlsfEncode(psEncC.indices.nlsfIndices[:], pNLSFQ15, psEncC.psNLSFCB, pNLSFWQW[:],
		nlsfMuQ20, psEncC.nlsfMSVQSurvivors, int(psEncC.indices.signalType))

	// Convert quantized NLSFs back to LPC coefficients.
	nlsf2a(predCoefQ12[1][:], pNLSFQ15, psEncC.predictLPCOrder)

	if doInterpolate {
		// Calculate the interpolated, quantized LSF vector for the first
		// half.
		interpolate(pNLSF0TempQ15[:], prevNLSFqQ15, pNLSFQ15, int(psEncC.indices.nlsfInterpCoefQ2), psEncC.predictLPCOrder)

		// Convert back to LPC coefficients.
		nlsf2a(predCoefQ12[0][:], pNLSF0TempQ15[:], psEncC.predictLPCOrder)
	} else {
		// Copy LPC coefficients for first half from second half.
		copy(predCoefQ12[0][:psEncC.predictLPCOrder], predCoefQ12[1][:psEncC.predictLPCOrder])
	}
}

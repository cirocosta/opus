// Ported from libopus 1.6.1 silk/decode_parameters.c.

package silk

// decodeParameters is silk_decode_parameters: decode parameters from
// payload.
// preserve the reference prediction, gain and numerical decision sequence
//
//revive:disable-next-line:cognitive-complexity
func (psDec *decoderState) decodeParameters(psDecCtrl *decoderControl, condCoding int) {
	var pNLSFQ15, pNLSF0Q15 [maxLPCOrder]int16

	// Dequant Gains.
	gainsDequant(&psDecCtrl.gainsQ16, &psDec.indices.gainsIndices,
		&psDec.lastGainIndex, condCoding == codeConditionally, psDec.nbSubfr)

	// Decode NLSFs.
	nlsfDecode(pNLSFQ15[:], psDec.indices.nlsfIndices[:], psDec.psNLSFCB)

	// Convert NLSF parameters to AR prediction filter coefficients.
	nlsf2a(psDecCtrl.predCoefQ12[1][:], pNLSFQ15[:], psDec.lpcOrder)

	// If just reset, e.g., because internal Fs changed, do not allow
	// interpolation. Improves the case of packet loss in the first frame
	// after a switch.
	if psDec.firstFrameAfterReset == 1 {
		psDec.indices.nlsfInterpCoefQ2 = 4
	}

	if psDec.indices.nlsfInterpCoefQ2 < 4 {
		// Calculation of the interpolated NLSF0 vector from the
		// interpolation factor, the previous NLSF1, and the current NLSF1.
		for i := 0; i < psDec.lpcOrder; i++ {
			pNLSF0Q15[i] = int16(
				int32(psDec.prevNLSFQ15[i]) + (int32(psDec.indices.nlsfInterpCoefQ2)*
					(int32(pNLSFQ15[i])-int32(psDec.prevNLSFQ15[i])))>>2,
			)
		}

		// Convert NLSF parameters to AR prediction filter coefficients.
		nlsf2a(psDecCtrl.predCoefQ12[0][:], pNLSF0Q15[:], psDec.lpcOrder)
	} else {
		// Copy LPC coefficients for first half from second half.
		copy(psDecCtrl.predCoefQ12[0][:psDec.lpcOrder], psDecCtrl.predCoefQ12[1][:psDec.lpcOrder])
	}

	copy(psDec.prevNLSFQ15[:psDec.lpcOrder], pNLSFQ15[:psDec.lpcOrder])

	// After a packet loss do BWE of LPC coefs.
	if psDec.lossCnt != 0 {
		bwexpander(psDecCtrl.predCoefQ12[0][:], psDec.lpcOrder, bweAfterLossQ16)
		bwexpander(psDecCtrl.predCoefQ12[1][:], psDec.lpcOrder, bweAfterLossQ16)
	}

	if psDec.indices.signalType == typeVoiced {
		// Decode pitch lags.
		// Decode pitch values.
		decodePitch(
			psDec.indices.lagIndex,
			psDec.indices.contourIndex,
			psDecCtrl.pitchL[:],
			psDec.fsKHz,
			psDec.nbSubfr,
		)

		// Decode Codebook Index.
		cbkPtrQ7 := ltpVqPtrsQ7[psDec.indices.perIndex] // set pointer to start of codebook
		for k := 0; k < psDec.nbSubfr; k++ {
			ix := psDec.indices.ltpIndex[k]
			for i := 0; i < ltpOrder; i++ {
				psDecCtrl.ltpCoefQ14[k*ltpOrder+i] = int16(lshift(int32(cbkPtrQ7[ix][i]), 7))
			}
		}

		// Decode LTP scaling.
		ix := psDec.indices.ltpScaleIndex
		psDecCtrl.ltpScaleQ14 = int(ltpScalesTableQ14[ix])
	} else {
		clear(psDecCtrl.pitchL[:psDec.nbSubfr])
		clear(psDecCtrl.ltpCoefQ14[:ltpOrder*psDec.nbSubfr])
		psDec.indices.perIndex = 0
		psDecCtrl.ltpScaleQ14 = 0
	}
}

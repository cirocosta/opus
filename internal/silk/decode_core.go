// Ported from libopus 1.6.1 silk/decode_core.c.

package silk

import "github.com/cirocosta/opus/internal/kernels"

// decodeCore is silk_decode_core: core decoder. Performs inverse NSQ
// operation LTP + LPC.
// preserve the reference prediction, gain and numerical decision sequence
//
//revive:disable-next-line:cognitive-complexity
func (psDec *decoderState) decodeCore(psDecCtrl *decoderControl, xq []int16, pulses []int16) {
	var aQ12Tmp [maxLPCOrder]int16
	lag := 0

	sLTP := psDec.scratch.sLTP[:psDec.ltpMemLength]
	sLTPQ15 := psDec.scratch.sLTPQ15[:psDec.ltpMemLength+psDec.frameLength]
	resQ14 := psDec.scratch.resQ14[:psDec.subfrLength]
	sLPCQ14 := psDec.scratch.sLPCQ14[:psDec.subfrLength+maxLPCOrder]

	nlsfInterpolationFlag := psDec.indices.nlsfInterpCoefQ2 < 1<<2

	psDec.decodeExcitation(pulses)

	// Copy LPC state.
	copy(sLPCQ14[:maxLPCOrder], psDec.sLPCQ14Buf[:])

	pexcQ14 := psDec.excQ14[:]
	pxq := xq
	sLTPBufIdx := psDec.ltpMemLength

	// Loop over subframes.
	for k := 0; k < psDec.nbSubfr; k++ {
		presQ14 := resQ14
		aQ12 := psDecCtrl.predCoefQ12[k>>1][:]

		// Preload LPC coefficients to array on stack. Gives small
		// performance gain.
		copy(aQ12Tmp[:psDec.lpcOrder], aQ12[:psDec.lpcOrder])
		bQ14 := psDecCtrl.ltpCoefQ14[k*ltpOrder : (k+1)*ltpOrder]
		signalType := int(psDec.indices.signalType)

		gainQ10 := psDecCtrl.gainsQ16[k] >> 6
		invGainQ31 := inverse32VarQ(psDecCtrl.gainsQ16[k], 47)

		// Calculate gain adjustment factor.
		var gainAdjQ16 int32
		if psDecCtrl.gainsQ16[k] != psDec.prevGainQ16 {
			gainAdjQ16 = div32VarQ(psDec.prevGainQ16, psDecCtrl.gainsQ16[k], 16)

			// Scale short term state.
			for i := 0; i < maxLPCOrder; i++ {
				sLPCQ14[i] = smulww(gainAdjQ16, sLPCQ14[i])
			}
		} else {
			gainAdjQ16 = int32(1) << 16
		}

		// Save inv_gain.
		psDec.prevGainQ16 = psDecCtrl.gainsQ16[k]

		// Avoid abrupt transition from voiced PLC to unvoiced normal
		// decoding.
		if psDec.lossCnt != 0 && psDec.prevSignalType == typeVoiced &&
			psDec.indices.signalType != typeVoiced && k < maxNbSubfr/2 {

			clear(bQ14)
			bQ14[ltpOrder/2] = 4096 // SILK_FIX_CONST( 0.25, 14 )

			signalType = typeVoiced
			psDecCtrl.pitchL[k] = psDec.lagPrev
		}

		if signalType == typeVoiced {
			// Voiced.
			lag = psDecCtrl.pitchL[k]

			// Re-whitening.
			if k == 0 || (k == 2 && nlsfInterpolationFlag) {
				// Rewhiten with new A coefs.
				startIdx := psDec.ltpMemLength - lag - psDec.lpcOrder - ltpOrder/2

				if k == 2 {
					copy(
						psDec.outBuf[psDec.ltpMemLength:psDec.ltpMemLength+2*psDec.subfrLength],
						xq[:2*psDec.subfrLength],
					)
				}

				lpcAnalysisFilter(sLTP[startIdx:], psDec.outBuf[startIdx+k*psDec.subfrLength:],
					aQ12, psDec.ltpMemLength-startIdx, psDec.lpcOrder)

				// After rewhitening the LTP state is unscaled.
				if k == 0 {
					// Do LTP downscaling to reduce inter-packet dependency.
					invGainQ31 = lshift(smulwb(invGainQ31, int32(psDecCtrl.ltpScaleQ14)), 2)
				}
				for i := 0; i < lag+ltpOrder/2; i++ {
					sLTPQ15[sLTPBufIdx-i-1] = smulwb(
						invGainQ31,
						int32(sLTP[psDec.ltpMemLength-i-1]),
					)
				}
			} else {
				// Update LTP state when Gain changes.
				if gainAdjQ16 != int32(1)<<16 {
					for i := 0; i < lag+ltpOrder/2; i++ {
						sLTPQ15[sLTPBufIdx-i-1] = smulww(gainAdjQ16, sLTPQ15[sLTPBufIdx-i-1])
					}
				}
			}
		}

		// Long-term prediction.
		if signalType == typeVoiced {
			// Set up pointer: predLag is the index of pred_lag_ptr[ 0 ].
			predLag := sLTPBufIdx - lag + ltpOrder/2
			for i := 0; i < psDec.subfrLength; i++ {
				// Unrolled loop.
				// Avoids introducing a bias because silk_SMLAWB() always
				// rounds to -inf.
				ltpPredQ13 := int32(2)
				ltpPredQ13 = smlawb(ltpPredQ13, sLTPQ15[predLag], int32(bQ14[0]))
				ltpPredQ13 = smlawb(ltpPredQ13, sLTPQ15[predLag-1], int32(bQ14[1]))
				ltpPredQ13 = smlawb(ltpPredQ13, sLTPQ15[predLag-2], int32(bQ14[2]))
				ltpPredQ13 = smlawb(ltpPredQ13, sLTPQ15[predLag-3], int32(bQ14[3]))
				ltpPredQ13 = smlawb(ltpPredQ13, sLTPQ15[predLag-4], int32(bQ14[4]))
				predLag++

				// Generate LPC excitation.
				presQ14[i] = addLshift32(pexcQ14[i], ltpPredQ13, 1)

				// Update states.
				sLTPQ15[sLTPBufIdx] = lshift(presQ14[i], 1)
				sLTPBufIdx++
			}
		} else {
			presQ14 = pexcQ14
		}

		kernels.Synthesize(
			sLPCQ14,
			presQ14,
			aQ12Tmp[:],
			gainQ10,
			pxq[:psDec.subfrLength],
			psDec.lpcOrder,
		)

		// Update LPC filter state.
		copy(sLPCQ14[:maxLPCOrder], sLPCQ14[psDec.subfrLength:psDec.subfrLength+maxLPCOrder])
		pexcQ14 = pexcQ14[psDec.subfrLength:]
		pxq = pxq[psDec.subfrLength:]
	}

	// Save LPC state.
	copy(psDec.sLPCQ14Buf[:], sLPCQ14[:maxLPCOrder])
}

// decodeExcitation reconstructs pulse offsets and seed-driven signs.
func (psDec *decoderState) decodeExcitation(pulses []int16) {
	offsetQ10 := int32(
		quantizationOffsetsQ10[psDec.indices.signalType>>1][psDec.indices.quantOffsetType],
	)

	// Decode excitation.
	randSeed := int32(psDec.indices.seed)
	for i := 0; i < psDec.frameLength; i++ {
		randSeed = silkRAND(randSeed)
		psDec.excQ14[i] = lshift(int32(pulses[i]), 14)
		if psDec.excQ14[i] > 0 {
			psDec.excQ14[i] -= quantLevelAdjustQ10 << 4
		} else if psDec.excQ14[i] < 0 {
			psDec.excQ14[i] += quantLevelAdjustQ10 << 4
		}
		psDec.excQ14[i] += offsetQ10 << 4
		if randSeed < 0 {
			psDec.excQ14[i] = -psDec.excQ14[i]
		}

		randSeed = add32ovflw(randSeed, int32(pulses[i]))
	}
}

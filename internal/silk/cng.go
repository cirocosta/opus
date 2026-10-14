// Ported from libopus 1.6.1 silk/CNG.c.

package silk

// cngExc is silk_CNG_exc: generates excitation for CNG LPC synthesis.
func cngExc(excQ14 []int32, excBufQ14 []int32, length int, randSeed *int32) {
	excMask := cngBufMaskMax
	for excMask > length {
		excMask = excMask >> 1
	}

	seed := *randSeed
	for i := 0; i < length; i++ {
		seed = silkRAND(seed)
		idx := int(seed>>24) & excMask
		excQ14[i] = excBufQ14[idx]
	}
	*randSeed = seed
}

// cngReset is silk_CNG_Reset.
func (psDec *decoderState) cngReset() {
	nlsfStepQ15 := int(div32_16(int16MAX, int32(psDec.lpcOrder+1)))
	nlsfAccQ15 := 0
	for i := 0; i < psDec.lpcOrder; i++ {
		nlsfAccQ15 += nlsfStepQ15
		psDec.sCNG.cngSmthNLSFQ15[i] = int16(nlsfAccQ15)
	}
	psDec.sCNG.cngSmthGainQ16 = 0
	psDec.sCNG.randSeed = 3176576
}

// cng is silk_CNG: updates CNG estimate, and applies the CNG when packet
// was lost.
// preserve the reference prediction, gain and numerical decision sequence
//
//revive:disable-next-line:cognitive-complexity
func (psDec *decoderState) cng(psDecCtrl *decoderControl, frame []int16, length int) {
	var aQ12 [maxLPCOrder]int16
	psCNG := &psDec.sCNG

	if psDec.fsKHz != psCNG.fsKHz {
		// Reset state.
		psDec.cngReset()

		psCNG.fsKHz = psDec.fsKHz
	}
	if psDec.lossCnt == 0 && psDec.prevSignalType == typeNoVoiceActivity {
		// Update CNG parameters.

		// Smoothing of LSF's.
		for i := 0; i < psDec.lpcOrder; i++ {
			psCNG.cngSmthNLSFQ15[i] += int16(
				smulwb(int32(psDec.prevNLSFQ15[i])-int32(psCNG.cngSmthNLSFQ15[i]), cngNLSFSmthQ16),
			)
		}
		// Find the subframe with the highest gain.
		maxGainQ16 := int32(0)
		subfr := 0
		for i := 0; i < psDec.nbSubfr; i++ {
			if psDecCtrl.gainsQ16[i] > maxGainQ16 {
				maxGainQ16 = psDecCtrl.gainsQ16[i]
				subfr = i
			}
		}
		// Update CNG excitation buffer with excitation from this subframe.
		copy(
			psCNG.cngExcBufQ14[psDec.subfrLength:psDec.nbSubfr*psDec.subfrLength],
			psCNG.cngExcBufQ14[:(psDec.nbSubfr-1)*psDec.subfrLength],
		)
		copy(
			psCNG.cngExcBufQ14[:psDec.subfrLength],
			psDec.excQ14[subfr*psDec.subfrLength:(subfr+1)*psDec.subfrLength],
		)

		// Smooth gains.
		for i := 0; i < psDec.nbSubfr; i++ {
			psCNG.cngSmthGainQ16 += smulwb(
				psDecCtrl.gainsQ16[i]-psCNG.cngSmthGainQ16,
				cngGainSmthQ16,
			)
			// If the smoothed gain is 3 dB greater than this subframe's
			// gain, use this subframe's gain to adapt faster.
			if smulww(psCNG.cngSmthGainQ16, cngGainSmthThresholdQ16) > psDecCtrl.gainsQ16[i] {
				psCNG.cngSmthGainQ16 = psDecCtrl.gainsQ16[i]
			}
		}
	}

	// Add CNG when packet is lost or during DTX.
	if psDec.lossCnt != 0 {
		cngSigQ14 := psDec.scratch.cngSig[:length+maxLPCOrder]

		// Generate CNG excitation.
		gainQ16 := smulww(int32(psDec.sPLC.randScaleQ14), psDec.sPLC.prevGainQ16[1])
		if gainQ16 >= (1<<21) || psCNG.cngSmthGainQ16 > (1<<23) {
			gainQ16 = smultt(gainQ16, gainQ16)
			gainQ16 = subLshift32(smultt(psCNG.cngSmthGainQ16, psCNG.cngSmthGainQ16), gainQ16, 5)
			gainQ16 = lshift(sqrtApprox(gainQ16), 16)
		} else {
			gainQ16 = smulww(gainQ16, gainQ16)
			gainQ16 = subLshift32(smulww(psCNG.cngSmthGainQ16, psCNG.cngSmthGainQ16), gainQ16, 5)
			gainQ16 = lshift(sqrtApprox(gainQ16), 8)
		}
		gainQ10 := gainQ16 >> 6

		cngExc(cngSigQ14[maxLPCOrder:], psCNG.cngExcBufQ14[:], length, &psCNG.randSeed)

		// Convert CNG NLSF to filter representation.
		nlsf2a(aQ12[:], psCNG.cngSmthNLSFQ15[:], psDec.lpcOrder)

		// Generate CNG signal, by synthesis filtering.
		copy(cngSigQ14[:maxLPCOrder], psCNG.cngSynthState[:])
		for i := 0; i < length; i++ {
			// Avoids introducing a bias because silk_SMLAWB() always rounds
			// to -inf.
			lpcPredQ10 := int32(psDec.lpcOrder >> 1)
			lpcPredQ10 = smlawb(lpcPredQ10, cngSigQ14[maxLPCOrder+i-1], int32(aQ12[0]))
			lpcPredQ10 = smlawb(lpcPredQ10, cngSigQ14[maxLPCOrder+i-2], int32(aQ12[1]))
			lpcPredQ10 = smlawb(lpcPredQ10, cngSigQ14[maxLPCOrder+i-3], int32(aQ12[2]))
			lpcPredQ10 = smlawb(lpcPredQ10, cngSigQ14[maxLPCOrder+i-4], int32(aQ12[3]))
			lpcPredQ10 = smlawb(lpcPredQ10, cngSigQ14[maxLPCOrder+i-5], int32(aQ12[4]))
			lpcPredQ10 = smlawb(lpcPredQ10, cngSigQ14[maxLPCOrder+i-6], int32(aQ12[5]))
			lpcPredQ10 = smlawb(lpcPredQ10, cngSigQ14[maxLPCOrder+i-7], int32(aQ12[6]))
			lpcPredQ10 = smlawb(lpcPredQ10, cngSigQ14[maxLPCOrder+i-8], int32(aQ12[7]))
			lpcPredQ10 = smlawb(lpcPredQ10, cngSigQ14[maxLPCOrder+i-9], int32(aQ12[8]))
			lpcPredQ10 = smlawb(lpcPredQ10, cngSigQ14[maxLPCOrder+i-10], int32(aQ12[9]))
			if psDec.lpcOrder == 16 {
				lpcPredQ10 = smlawb(lpcPredQ10, cngSigQ14[maxLPCOrder+i-11], int32(aQ12[10]))
				lpcPredQ10 = smlawb(lpcPredQ10, cngSigQ14[maxLPCOrder+i-12], int32(aQ12[11]))
				lpcPredQ10 = smlawb(lpcPredQ10, cngSigQ14[maxLPCOrder+i-13], int32(aQ12[12]))
				lpcPredQ10 = smlawb(lpcPredQ10, cngSigQ14[maxLPCOrder+i-14], int32(aQ12[13]))
				lpcPredQ10 = smlawb(lpcPredQ10, cngSigQ14[maxLPCOrder+i-15], int32(aQ12[14]))
				lpcPredQ10 = smlawb(lpcPredQ10, cngSigQ14[maxLPCOrder+i-16], int32(aQ12[15]))
			}

			// Update states.
			cngSigQ14[maxLPCOrder+i] = addSat32(
				cngSigQ14[maxLPCOrder+i],
				lshiftSat32(lpcPredQ10, 4),
			)

			// Scale with Gain and add to input signal.
			frame[i] = addSat16(
				frame[i],
				int16(sat16(rshiftRound(smulww(cngSigQ14[maxLPCOrder+i], gainQ10), 8))),
			)

		}
		copy(psCNG.cngSynthState[:], cngSigQ14[length:length+maxLPCOrder])
	} else {
		clear(psCNG.cngSynthState[:psDec.lpcOrder])
	}
}

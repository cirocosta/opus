// Ported from libopus 1.6.1 silk/PLC.c and silk/PLC.h.

package silk

// PLC.h.
const (
	plcBWECoefQ16           = 64881 // SILK_FIX_CONST( BWE_COEF, 16 ), BWE_COEF = 0.99
	vPitchGainStartMinQ14   = 11469 // 0.7 in Q14
	vPitchGainStartMaxQ14   = 15565 // 0.95 in Q14
	maxPitchLagMs           = 18
	randBufSize             = 128
	randBufMask             = randBufSize - 1
	log2InvLPCGainHighThres = 3   // 2^3 = 8 dB LPC gain
	log2InvLPCGainLowThres  = 8   // 2^8 = 24 dB LPC gain
	pitchDriftFacQ16        = 655 // 0.01 in Q16
	plcNbAtt                = 2   // NB_ATT
)

var (
	harmAttQ15            = [plcNbAtt]int16{32440, 31130} // 0.99, 0.95
	plcRandAttenuateVQ15  = [plcNbAtt]int16{31130, 26214} // 0.95, 0.8
	plcRandAttenuateUVQ15 = [plcNbAtt]int16{32440, 29491} // 0.99, 0.9
)

// plcReset is silk_PLC_Reset.
func (psDec *decoderState) plcReset() {
	psDec.sPLC.pitchLQ8 = lshift(int32(psDec.frameLength), 8-1)
	psDec.sPLC.prevGainQ16[0] = 1 << 16 // SILK_FIX_CONST( 1, 16 )
	psDec.sPLC.prevGainQ16[1] = 1 << 16
	psDec.sPLC.subfrLength = 20
	psDec.sPLC.nbSubfr = 2
}

// plc is silk_PLC: the PLC control function.
func (psDec *decoderState) plc(psDecCtrl *decoderControl, frame []int16, lost bool) {
	// PLC control function.
	if psDec.fsKHz != psDec.sPLC.fsKHz {
		psDec.plcReset()
		psDec.sPLC.fsKHz = psDec.fsKHz
	}

	if lost {
		// Generate Signal.
		psDec.plcConceal(psDecCtrl, frame)

		psDec.lossCnt++
	} else {
		// Update state.
		psDec.plcUpdate(psDecCtrl)
	}
}

// plcUpdate is silk_PLC_update: update state of PLC.
func (psDec *decoderState) plcUpdate(psDecCtrl *decoderControl) {
	psPLC := &psDec.sPLC

	// Update parameters used in case of packet loss.
	psDec.prevSignalType = int(psDec.indices.signalType)
	ltpGainQ14 := int32(0)
	if psDec.indices.signalType == typeVoiced {
		// Find the parameters for the last subframe which contains a pitch
		// pulse.
		for j := 0; j*psDec.subfrLength < psDecCtrl.pitchL[psDec.nbSubfr-1]; j++ {
			if j == psDec.nbSubfr {
				break
			}
			tempLTPGainQ14 := int32(0)
			for i := 0; i < ltpOrder; i++ {
				tempLTPGainQ14 += int32(psDecCtrl.ltpCoefQ14[(psDec.nbSubfr-1-j)*ltpOrder+i])
			}
			if tempLTPGainQ14 > ltpGainQ14 {
				ltpGainQ14 = tempLTPGainQ14
				off := int(smulbb(int32(psDec.nbSubfr-1-j), ltpOrder))
				copy(psPLC.ltpCoefQ14[:], psDecCtrl.ltpCoefQ14[off:off+ltpOrder])

				psPLC.pitchLQ8 = lshift(int32(psDecCtrl.pitchL[psDec.nbSubfr-1-j]), 8)
			}
		}

		clear(psPLC.ltpCoefQ14[:])
		psPLC.ltpCoefQ14[ltpOrder/2] = int16(ltpGainQ14)

		// Limit LT coefs.
		if ltpGainQ14 < vPitchGainStartMinQ14 {
			tmp := lshift(vPitchGainStartMinQ14, 10)
			scaleQ10 := div32(tmp, max32(ltpGainQ14, 1))
			for i := 0; i < ltpOrder; i++ {
				psPLC.ltpCoefQ14[i] = int16(smulbb(int32(psPLC.ltpCoefQ14[i]), scaleQ10) >> 10)
			}
		} else if ltpGainQ14 > vPitchGainStartMaxQ14 {
			tmp := lshift(vPitchGainStartMaxQ14, 14)
			scaleQ14 := div32(tmp, max32(ltpGainQ14, 1))
			for i := 0; i < ltpOrder; i++ {
				psPLC.ltpCoefQ14[i] = int16(smulbb(int32(psPLC.ltpCoefQ14[i]), scaleQ14) >> 14)
			}
		}
	} else {
		psPLC.pitchLQ8 = lshift(smulbb(int32(psDec.fsKHz), 18), 8)
		clear(psPLC.ltpCoefQ14[:])
	}

	// Save LPC coefficients.
	copy(psPLC.prevLPCQ12[:psDec.lpcOrder], psDecCtrl.predCoefQ12[1][:psDec.lpcOrder])
	psPLC.prevLTPScaleQ14 = int16(psDecCtrl.ltpScaleQ14)

	// Save last two gains.
	copy(psPLC.prevGainQ16[:], psDecCtrl.gainsQ16[psDec.nbSubfr-2:psDec.nbSubfr])

	psPLC.subfrLength = psDec.subfrLength
	psPLC.nbSubfr = psDec.nbSubfr
}

// plcEnergy is silk_PLC_energy.
func (psDec *decoderState) plcEnergy(excQ14 []int32, prevGainQ10 *[2]int32, subfrLength, nbSubfr int) (energy1 int32, shift1 int, energy2 int32, shift2 int) {
	excBuf := psDec.scratch.excBuf[:2*subfrLength]

	// Find random noise component.
	// Scale previous excitation signal.
	excBufPtr := excBuf
	for k := 0; k < 2; k++ {
		for i := 0; i < subfrLength; i++ {
			excBufPtr[i] = int16(sat16(smulww(excQ14[i+(k+nbSubfr-2)*subfrLength], prevGainQ10[k]) >> 8))
		}
		excBufPtr = excBufPtr[subfrLength:]
	}

	// Find the subframe with lowest energy of the last two and use that as
	// random noise generator.
	energy1, shift1 = sumSqrShift(excBuf, subfrLength)
	energy2, shift2 = sumSqrShift(excBuf[subfrLength:], subfrLength)
	return
}

// plcConceal is silk_PLC_conceal.
func (psDec *decoderState) plcConceal(psDecCtrl *decoderControl, frame []int16) {
	var aQ12 [maxLPCOrder]int16
	var prevGainQ10 [2]int32
	var randPtr []int32
	psPLC := &psDec.sPLC

	sLTPQ14 := psDec.scratch.sLTPQ15[:psDec.ltpMemLength+psDec.frameLength]
	sLTP := psDec.scratch.sLTP[:psDec.ltpMemLength]

	prevGainQ10[0] = psPLC.prevGainQ16[0] >> 6
	prevGainQ10[1] = psPLC.prevGainQ16[1] >> 6

	if psDec.firstFrameAfterReset != 0 {
		clear(psPLC.prevLPCQ12[:])
	}

	energy1, shift1, energy2, shift2 := psDec.plcEnergy(psDec.excQ14[:], &prevGainQ10, psDec.subfrLength, psDec.nbSubfr)

	if energy1>>uint(shift2) < energy2>>uint(shift1) {
		// First sub-frame has lowest energy.
		randPtr = psDec.excQ14[maxInt(0, (psPLC.nbSubfr-1)*psPLC.subfrLength-randBufSize):]
	} else {
		// Second sub-frame has lowest energy.
		randPtr = psDec.excQ14[maxInt(0, psPLC.nbSubfr*psPLC.subfrLength-randBufSize):]
	}

	// Set up Gain to random noise component.
	bQ14 := psPLC.ltpCoefQ14[:]
	randScaleQ14 := psPLC.randScaleQ14

	// Set up attenuation gains.
	harmGainQ15 := int32(harmAttQ15[minInt(plcNbAtt-1, psDec.lossCnt)])
	var randGainQ15 int32
	if psDec.prevSignalType == typeVoiced {
		randGainQ15 = int32(plcRandAttenuateVQ15[minInt(plcNbAtt-1, psDec.lossCnt)])
	} else {
		randGainQ15 = int32(plcRandAttenuateUVQ15[minInt(plcNbAtt-1, psDec.lossCnt)])
	}

	// LPC concealment. Apply BWE to previous LPC.
	bwexpander(psPLC.prevLPCQ12[:], psDec.lpcOrder, plcBWECoefQ16)

	// Preload LPC coefficients to array on stack. Gives small performance
	// gain.
	copy(aQ12[:psDec.lpcOrder], psPLC.prevLPCQ12[:psDec.lpcOrder])

	// First Lost frame.
	if psDec.lossCnt == 0 {
		randScaleQ14 = 1 << 14

		// Reduce random noise Gain for voiced frames.
		if psDec.prevSignalType == typeVoiced {
			for i := 0; i < ltpOrder; i++ {
				randScaleQ14 -= bQ14[i]
			}
			randScaleQ14 = max16(3277, randScaleQ14) // 0.2
			randScaleQ14 = int16(smulbb(int32(randScaleQ14), int32(psPLC.prevLTPScaleQ14)) >> 14)
		} else {
			// Reduce random noise for unvoiced frames with high LPC gain.
			invGainQ30 := lpcInversePredGain(psPLC.prevLPCQ12[:], psDec.lpcOrder)

			downScaleQ30 := min32((int32(1)<<30)>>log2InvLPCGainHighThres, invGainQ30)
			downScaleQ30 = max32((int32(1)<<30)>>log2InvLPCGainLowThres, downScaleQ30)
			downScaleQ30 = lshift(downScaleQ30, log2InvLPCGainHighThres)

			randGainQ15 = smulwb(downScaleQ30, randGainQ15) >> 14
		}
	}

	randSeed := psPLC.randSeed
	lag := int(rshiftRound(psPLC.pitchLQ8, 8))
	sLTPBufIdx := psDec.ltpMemLength

	// Rewhiten LTP state.
	idx := psDec.ltpMemLength - lag - psDec.lpcOrder - ltpOrder/2
	lpcAnalysisFilter(sLTP[idx:], psDec.outBuf[idx:], aQ12[:], psDec.ltpMemLength-idx, psDec.lpcOrder)
	// Scale LTP state.
	invGainQ30 := inverse32VarQ(psPLC.prevGainQ16[1], 46)
	invGainQ30 = min32(invGainQ30, int32MAX>>1)
	for i := idx + psDec.lpcOrder; i < psDec.ltpMemLength; i++ {
		sLTPQ14[i] = smulwb(invGainQ30, int32(sLTP[i]))
	}

	// LTP synthesis filtering.
	for k := 0; k < psDec.nbSubfr; k++ {
		// Set up pointer: predLag is the index of pred_lag_ptr[ 0 ].
		predLag := sLTPBufIdx - lag + ltpOrder/2
		for i := 0; i < psDec.subfrLength; i++ {
			// Unrolled loop.
			// Avoids introducing a bias because silk_SMLAWB() always rounds
			// to -inf.
			ltpPredQ12 := int32(2)
			ltpPredQ12 = smlawb(ltpPredQ12, sLTPQ14[predLag], int32(bQ14[0]))
			ltpPredQ12 = smlawb(ltpPredQ12, sLTPQ14[predLag-1], int32(bQ14[1]))
			ltpPredQ12 = smlawb(ltpPredQ12, sLTPQ14[predLag-2], int32(bQ14[2]))
			ltpPredQ12 = smlawb(ltpPredQ12, sLTPQ14[predLag-3], int32(bQ14[3]))
			ltpPredQ12 = smlawb(ltpPredQ12, sLTPQ14[predLag-4], int32(bQ14[4]))
			predLag++

			// Generate LPC excitation.
			randSeed = silkRAND(randSeed)
			idx = int(randSeed>>25) & randBufMask
			sLTPQ14[sLTPBufIdx] = lshift(smlawb(ltpPredQ12, randPtr[idx], int32(randScaleQ14)), 2)
			sLTPBufIdx++
		}

		// Gradually reduce LTP gain.
		for j := 0; j < ltpOrder; j++ {
			bQ14[j] = int16(smulbb(harmGainQ15, int32(bQ14[j])) >> 15)
		}
		// Gradually reduce excitation gain.
		randScaleQ14 = int16(smulbb(int32(randScaleQ14), randGainQ15) >> 15)

		// Slowly increase pitch lag.
		psPLC.pitchLQ8 = smlawb(psPLC.pitchLQ8, psPLC.pitchLQ8, pitchDriftFacQ16)
		psPLC.pitchLQ8 = min32(psPLC.pitchLQ8, lshift(smulbb(maxPitchLagMs, int32(psDec.fsKHz)), 8))
		lag = int(rshiftRound(psPLC.pitchLQ8, 8))
	}

	// LPC synthesis filtering.
	sLPCQ14 := sLTPQ14[psDec.ltpMemLength-maxLPCOrder:]

	// Copy LPC state.
	copy(sLPCQ14[:maxLPCOrder], psDec.sLPCQ14Buf[:])

	for i := 0; i < psDec.frameLength; i++ {
		// Partly unrolled.
		// Avoids introducing a bias because silk_SMLAWB() always rounds to
		// -inf.
		lpcPredQ10 := int32(psDec.lpcOrder >> 1)
		lpcPredQ10 = smlawb(lpcPredQ10, sLPCQ14[maxLPCOrder+i-1], int32(aQ12[0]))
		lpcPredQ10 = smlawb(lpcPredQ10, sLPCQ14[maxLPCOrder+i-2], int32(aQ12[1]))
		lpcPredQ10 = smlawb(lpcPredQ10, sLPCQ14[maxLPCOrder+i-3], int32(aQ12[2]))
		lpcPredQ10 = smlawb(lpcPredQ10, sLPCQ14[maxLPCOrder+i-4], int32(aQ12[3]))
		lpcPredQ10 = smlawb(lpcPredQ10, sLPCQ14[maxLPCOrder+i-5], int32(aQ12[4]))
		lpcPredQ10 = smlawb(lpcPredQ10, sLPCQ14[maxLPCOrder+i-6], int32(aQ12[5]))
		lpcPredQ10 = smlawb(lpcPredQ10, sLPCQ14[maxLPCOrder+i-7], int32(aQ12[6]))
		lpcPredQ10 = smlawb(lpcPredQ10, sLPCQ14[maxLPCOrder+i-8], int32(aQ12[7]))
		lpcPredQ10 = smlawb(lpcPredQ10, sLPCQ14[maxLPCOrder+i-9], int32(aQ12[8]))
		lpcPredQ10 = smlawb(lpcPredQ10, sLPCQ14[maxLPCOrder+i-10], int32(aQ12[9]))
		for j := 10; j < psDec.lpcOrder; j++ {
			lpcPredQ10 = smlawb(lpcPredQ10, sLPCQ14[maxLPCOrder+i-j-1], int32(aQ12[j]))
		}

		// Add prediction to LPC excitation.
		sLPCQ14[maxLPCOrder+i] = addSat32(sLPCQ14[maxLPCOrder+i], lshiftSat32(lpcPredQ10, 4))

		// Scale with Gain.
		frame[i] = int16(sat16(sat16(rshiftRound(smulww(sLPCQ14[maxLPCOrder+i], prevGainQ10[1]), 8))))
	}

	// Save LPC state.
	copy(psDec.sLPCQ14Buf[:], sLPCQ14[psDec.frameLength:psDec.frameLength+maxLPCOrder])

	// Update states.
	psPLC.randSeed = randSeed
	psPLC.randScaleQ14 = randScaleQ14
	for i := 0; i < maxNbSubfr; i++ {
		psDecCtrl.pitchL[i] = lag
	}
}

// plcGlueFrames is silk_PLC_glue_frames: glues concealed frames with new
// good received frames.
func (psDec *decoderState) plcGlueFrames(frame []int16, length int) {
	psPLC := &psDec.sPLC

	if psDec.lossCnt != 0 {
		// Calculate energy in concealed residual.
		psPLC.concEnergy, psPLC.concEnergyShift = sumSqrShift(frame, length)

		psPLC.lastFrameLost = 1
	} else {
		if psDec.sPLC.lastFrameLost != 0 {
			// Calculate residual in decoded signal if last frame was lost.
			energy, energyShift := sumSqrShift(frame, length)

			// Normalize energies.
			if energyShift > psPLC.concEnergyShift {
				psPLC.concEnergy = psPLC.concEnergy >> uint(energyShift-psPLC.concEnergyShift)
			} else if energyShift < psPLC.concEnergyShift {
				energy = energy >> uint(psPLC.concEnergyShift-energyShift)
			}

			// Fade in the energy difference.
			if energy > psPLC.concEnergy {
				lz := clz32(psPLC.concEnergy)
				lz = lz - 1
				psPLC.concEnergy = lshift(psPLC.concEnergy, int(lz))
				energy = energy >> uint(max32(24-lz, 0))

				fracQ24 := div32(psPLC.concEnergy, max32(energy, 1))

				gainQ16 := lshift(sqrtApprox(fracQ24), 4)
				slopeQ16 := div32_16((int32(1)<<16)-gainQ16, int32(length))
				// Make slope 4x steeper to avoid missing onsets after DTX.
				slopeQ16 = lshift(slopeQ16, 2)
				for i := 0; i < length; i++ {
					frame[i] = int16(smulwb(gainQ16, int32(frame[i])))
					gainQ16 += slopeQ16
					if gainQ16 > int32(1)<<16 {
						break
					}
				}
			}
		}
		psPLC.lastFrameLost = 0
	}
}

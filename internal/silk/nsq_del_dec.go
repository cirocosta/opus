// Ported from libopus 1.6.1 silk/NSQ_del_dec.c.

package silk

import "github.com/cirocosta/opus/internal/kernels"

// nsqDelDecStruct holds one delayed-decision branch's LPC history and
// noise-shaping state. Predictor history and ring state are copied separately
// when a surviving branch replaces another branch.
type nsqDelDecStruct struct {
	sLPCQ14 [maxSubFrameLength + nsqLPCBufLength]int32
	nsqDelDecRest
}

// nsqDelDecRest holds the NSQ_del_dec_struct fields after sLPC_Q14.
type nsqDelDecRest struct {
	randState [decisionDelay]int32
	qQ10      [decisionDelay]int32
	xqQ14     [decisionDelay]int32
	predQ15   [decisionDelay]int32
	shapeQ14  [decisionDelay]int32
	sAR2Q14   [maxShapeLPCOrder]int32
	lfARQ14   int32
	diffQ14   int32
	seed      int32
	seedInit  int32
	rdQ10     int32
}

// copyFrom copies the live predictor window and delayed-decision state.
// i identifies the first history sample needed for the next prediction.
func (d *nsqDelDecStruct) copyFrom(s *nsqDelDecStruct, i int) {
	// only the preceding 16 samples are live; the current and future
	// slots are written before any prediction can read them.
	copy(d.sLPCQ14[i:i+nsqLPCBufLength], s.sLPCQ14[i:i+nsqLPCBufLength])
	d.nsqDelDecRest = s.nsqDelDecRest
}

// nsqSampleStruct is NSQ_sample_struct.
type nsqSampleStruct struct {
	qQ10       int32
	rdQ10      int32
	xqQ14      int32
	lfARQ14    int32
	diffQ14    int32
	sLTPShpQ14 int32
	lpcExcQ14  int32
}

// nsqDelDec is silk_NSQ_del_dec (silk_NSQ_del_dec_c): noise shaping
// quantization with delayed decision.
// preserve the reference prediction, gain and numerical decision sequence
//
//revive:disable-next-line:cognitive-complexity
func (psEncC *encoderState) nsqDelDec(
	nsq *nsqState,
	psIndices *sideInfoIndices,
	x16 []int16,
	pulses []int8,
	p *nsqParams,
	sc *nsqScratch,
) {
	// Set unvoiced lag to the previous one, overwrite later for voiced.
	lag := nsq.lagPrev

	// Initialize delayed decision states.
	psDelDec := sc.psDelDec[:psEncC.nStatesDelayedDecision]
	for k := range psDelDec {
		psDelDec[k] = nsqDelDecStruct{}
	}
	for k := 0; k < psEncC.nStatesDelayedDecision; k++ {
		psDD := &psDelDec[k]
		psDD.seed = int32((k + int(psIndices.seed)) & 3)
		psDD.seedInit = psDD.seed
		psDD.rdQ10 = 0
		psDD.lfARQ14 = nsq.sLFARShpQ14
		psDD.diffQ14 = nsq.sDiffShpQ14
		psDD.shapeQ14[0] = nsq.sLTPShpQ14[psEncC.ltpMemLength-1]
		copy(psDD.sLPCQ14[:nsqLPCBufLength], nsq.sLPCQ14[:nsqLPCBufLength])
		psDD.sAR2Q14 = nsq.sAR2Q14
	}

	offsetQ10 := int(quantizationOffsetsQ10[psIndices.signalType>>1][psIndices.quantOffsetType])
	smplBufIdx := 0 // index of oldest samples

	decDelay := minInt(decisionDelay, psEncC.subfrLength)

	// For voiced frames limit the decision delay to lower than the pitch
	// lag.
	if psIndices.signalType == typeVoiced {
		for k := 0; k < psEncC.nbSubfr; k++ {
			decDelay = minInt(decDelay, p.pitchL[k]-ltpOrder/2-1)
		}
	} else {
		if lag > 0 {
			decDelay = minInt(decDelay, lag-ltpOrder/2-1)
		}
	}

	lsfInterpolationFlag := 1
	if psIndices.nlsfInterpCoefQ2 == 4 {
		lsfInterpolationFlag = 0
	}

	sLTPQ15 := sc.sLTPQ15[:psEncC.ltpMemLength+psEncC.frameLength]
	sLTP := sc.sLTP[:psEncC.ltpMemLength+psEncC.frameLength]
	xScQ10 := sc.xScQ10[:psEncC.subfrLength]
	delayedGainQ10 := sc.delayedGainQ10[:]
	// Set up pointers to start of sub frame.
	pxq := psEncC.ltpMemLength
	nsq.sLTPShpBufIdx = psEncC.ltpMemLength
	nsq.sLTPBufIdx = psEncC.ltpMemLength
	x16Off := 0
	pulsesOff := 0
	subfr := 0
	for k := 0; k < psEncC.nbSubfr; k++ {
		aQ12 := p.predCoefQ12[(k>>1)|(1-lsfInterpolationFlag)][:]
		bQ14 := p.ltpCoefQ14[k*ltpOrder:]
		arShpQ13 := p.arQ13[k*maxShapeLPCOrder:]

		// Noise shape parameters.
		harmShapeFIRPackedQ14 := int32(p.harmShapeGainQ14[k] >> 2)
		harmShapeFIRPackedQ14 |= lshift(int32(p.harmShapeGainQ14[k]>>1), 16)

		nsq.rewhiteFlag = 0
		if psIndices.signalType == typeVoiced {
			// Voiced.
			lag = p.pitchL[k]

			// Re-whitening.
			if k&(3-(lsfInterpolationFlag<<1)) == 0 {
				if k == 2 {
					// RESET DELAYED DECISIONS.
					// Find winner.
					rdMinQ10 := psDelDec[0].rdQ10
					winnerInd := 0
					for i := 1; i < psEncC.nStatesDelayedDecision; i++ {
						if psDelDec[i].rdQ10 < rdMinQ10 {
							rdMinQ10 = psDelDec[i].rdQ10
							winnerInd = i
						}
					}
					for i := 0; i < psEncC.nStatesDelayedDecision; i++ {
						if i != winnerInd {
							psDelDec[i].rdQ10 += int32MAX >> 4
						}
					}

					// Copy final part of signals from winner state to
					// output and long-term filter states.
					psDD := &psDelDec[winnerInd]
					lastSmpleIdx := (smplBufIdx + decDelay) % decisionDelay
					for i := 0; i < decDelay; i++ {
						lastSmpleIdx--
						if lastSmpleIdx < 0 {
							lastSmpleIdx += decisionDelay
						}
						pulses[pulsesOff+i-decDelay] = int8(
							rshiftRound(psDD.qQ10[lastSmpleIdx], 10),
						)
						nsq.xq[pxq+i-decDelay] = int16(sat16(rshiftRound(
							smulww(psDD.xqQ14[lastSmpleIdx], p.gainsQ16[1]), 14)))
						nsq.sLTPShpQ14[nsq.sLTPShpBufIdx-decDelay+i] = psDD.shapeQ14[lastSmpleIdx]
					}

					subfr = 0
				}

				// Rewhiten with new A coefs.
				startIdx := psEncC.ltpMemLength - lag - psEncC.predictLPCOrder - ltpOrder/2

				lpcAnalysisFilter(sLTP[startIdx:], nsq.xq[startIdx+k*psEncC.subfrLength:],
					aQ12, psEncC.ltpMemLength-startIdx, psEncC.predictLPCOrder)

				nsq.sLTPBufIdx = psEncC.ltpMemLength
				nsq.rewhiteFlag = 1
			}
		}

		psEncC.nsqDelDecScaleStates(
			nsq,
			psDelDec,
			x16[x16Off:],
			xScQ10,
			sLTP,
			sLTPQ15,
			k,
			psEncC.nStatesDelayedDecision,
			p.ltpScaleQ14,
			&p.gainsQ16,
			&p.pitchL,
			int(psIndices.signalType),
			decDelay,
		)

		nsq.noiseShapeQuantizerDelDec(
			psDelDec,
			int(psIndices.signalType),
			xScQ10,
			pulses,
			pulsesOff,
			pxq,
			sLTPQ15,
			delayedGainQ10,
			aQ12,
			bQ14,
			arShpQ13,
			lag,
			harmShapeFIRPackedQ14,
			p.tiltQ14[k],
			p.lfShpQ14[k],
			p.gainsQ16[k],
			p.lambdaQ10,
			offsetQ10,
			psEncC.subfrLength,
			subfr,
			psEncC.shapingLPCOrder,
			psEncC.predictLPCOrder,
			psEncC.warpingQ16,
			psEncC.nStatesDelayedDecision,
			&smplBufIdx,
			decDelay,
			&sc.psSampleState,
		)
		subfr++

		x16Off += psEncC.subfrLength
		pulsesOff += psEncC.subfrLength
		pxq += psEncC.subfrLength
	}

	// Find winner.
	rdMinQ10 := psDelDec[0].rdQ10
	winnerInd := 0
	for k := 1; k < psEncC.nStatesDelayedDecision; k++ {
		if psDelDec[k].rdQ10 < rdMinQ10 {
			rdMinQ10 = psDelDec[k].rdQ10
			winnerInd = k
		}
	}

	// Copy final part of signals from winner state to output and
	// long-term filter states.
	psDD := &psDelDec[winnerInd]
	psIndices.seed = int8(psDD.seedInit)
	lastSmpleIdx := (smplBufIdx + decDelay) % decisionDelay
	gainQ10 := p.gainsQ16[psEncC.nbSubfr-1] >> 6
	for i := 0; i < decDelay; i++ {
		lastSmpleIdx--
		if lastSmpleIdx < 0 {
			lastSmpleIdx += decisionDelay
		}

		pulses[pulsesOff+i-decDelay] = int8(rshiftRound(psDD.qQ10[lastSmpleIdx], 10))
		nsq.xq[pxq+i-decDelay] = int16(sat16(rshiftRound(
			smulww(psDD.xqQ14[lastSmpleIdx], gainQ10), 8)))
		nsq.sLTPShpQ14[nsq.sLTPShpBufIdx-decDelay+i] = psDD.shapeQ14[lastSmpleIdx]
	}
	copy(
		nsq.sLPCQ14[:nsqLPCBufLength],
		psDD.sLPCQ14[psEncC.subfrLength:psEncC.subfrLength+nsqLPCBufLength],
	)
	nsq.sAR2Q14 = psDD.sAR2Q14

	// Update states.
	nsq.sLFARShpQ14 = psDD.lfARQ14
	nsq.sDiffShpQ14 = psDD.diffQ14
	nsq.lagPrev = p.pitchL[psEncC.nbSubfr-1]

	// Save quantized speech signal.
	copy(
		nsq.xq[:psEncC.ltpMemLength],
		nsq.xq[psEncC.frameLength:psEncC.frameLength+psEncC.ltpMemLength],
	)
	copy(
		nsq.sLTPShpQ14[:psEncC.ltpMemLength],
		nsq.sLTPShpQ14[psEncC.frameLength:psEncC.frameLength+psEncC.ltpMemLength],
	)
}

// noiseShapeQuantizerDelDec is silk_noise_shape_quantizer_del_dec: noise
// shape quantizer for one subframe. pulses[pulsesOff+i] and
// nsq.xq[xqOff+i] are pulses[ i ] and xq[ i ] in C.
// preserve the reference prediction, gain and numerical decision sequence
//
//revive:disable-next-line:cognitive-complexity
func (nsq *nsqState) noiseShapeQuantizerDelDec(
	psDelDec []nsqDelDecStruct,
	signalType int,
	xQ10 []int32,
	pulses []int8,
	pulsesOff, xqOff int,
	sLTPQ15 []int32,
	delayedGainQ10 []int32,
	aQ12, bQ14, arShpQ13 []int16,
	lag int,
	harmShapeFIRPackedQ14 int32,
	tiltQ14 int,
	lfShpQ14, gainQ16 int32,
	lambdaQ10, offsetQ10, length,
	subfr, shapingLPCOrder, predictLPCOrder, warpingQ16, nStatesDelayedDecision int,
	smplBufIdx *int,
	decDelay int,
	psSampleState *[maxDelDecStates][2]nsqSampleStruct,
) {
	shpLag := nsq.sLTPShpBufIdx - lag + harmShapeFIRTaps/2
	predLag := nsq.sLTPBufIdx - lag + ltpOrder/2
	gainQ10 := gainQ16 >> 6

	for i := 0; i < length; i++ {
		// Perform common calculations used in all states.

		// Long-term prediction.
		var ltpPredQ14 int32
		if signalType == typeVoiced {
			// Unrolled loop.
			// Avoids introducing a bias because silk_SMLAWB() always rounds
			// to -inf.
			ltpPredQ14 = 2
			ltpPredQ14 = smlawb(ltpPredQ14, sLTPQ15[predLag-0], int32(bQ14[0]))
			ltpPredQ14 = smlawb(ltpPredQ14, sLTPQ15[predLag-1], int32(bQ14[1]))
			ltpPredQ14 = smlawb(ltpPredQ14, sLTPQ15[predLag-2], int32(bQ14[2]))
			ltpPredQ14 = smlawb(ltpPredQ14, sLTPQ15[predLag-3], int32(bQ14[3]))
			ltpPredQ14 = smlawb(ltpPredQ14, sLTPQ15[predLag-4], int32(bQ14[4]))
			ltpPredQ14 = lshift(ltpPredQ14, 1) // Q13 -> Q14
			predLag++
		} else {
			ltpPredQ14 = 0
		}

		// Long-term shaping.
		var nLTPQ14 int32
		if lag > 0 {
			// Symmetric, packed FIR coefficients.
			nLTPQ14 = smulwb(
				addSat32(nsq.sLTPShpQ14[shpLag-0], nsq.sLTPShpQ14[shpLag-2]),
				harmShapeFIRPackedQ14,
			)
			nLTPQ14 = smlawt(nLTPQ14, nsq.sLTPShpQ14[shpLag-1], harmShapeFIRPackedQ14)
			nLTPQ14 = subLshift32(ltpPredQ14, nLTPQ14, 2) // Q12 -> Q14
			shpLag++
		} else {
			nLTPQ14 = 0
		}

		for k := 0; k < nStatesDelayedDecision; k++ {
			// Delayed decision state.
			psDD := &psDelDec[k]

			// Sample state.
			psSS := &psSampleState[k]

			// Generate dither.
			psDD.seed = silkRAND(psDD.seed)

			// Pointer used in short term prediction and shaping.
			psLPCQ14 := nsqLPCBufLength - 1 + i
			// Short-term prediction.
			lpcPredQ14 := noiseShapeQuantizerShortPrediction(
				psDD.sLPCQ14[:],
				psLPCQ14,
				aQ12,
				predictLPCOrder,
			)
			lpcPredQ14 = lshift(lpcPredQ14, 4) // Q10 -> Q14

			// Preserve the ordered allpass chain and each fixed-point wrap.
			nARQ14 := kernels.WarpedFeedback(
				psDD.sAR2Q14[:],
				arShpQ13[:shapingLPCOrder],
				psDD.diffQ14,
				int32(warpingQ16),
			)

			nARQ14 = lshift(nARQ14, 1)                            // Q11 -> Q12
			nARQ14 = smlawb(nARQ14, psDD.lfARQ14, int32(tiltQ14)) // Q12
			nARQ14 = lshift(nARQ14, 2)                            // Q12 -> Q14

			nLFQ14 := smulwb(psDD.shapeQ14[*smplBufIdx], lfShpQ14) // Q12
			nLFQ14 = smlawt(nLFQ14, psDD.lfARQ14, lfShpQ14)        // Q12
			nLFQ14 = lshift(nLFQ14, 2)                             // Q12 -> Q14

			// Input minus prediction plus noise feedback.
			// r = x[ i ] - LTP_pred - LPC_pred + n_AR + n_Tilt + n_LF + n_LTP.
			tmp1 := addSat32(nARQ14, nLFQ14)        // Q14
			tmp2 := add32ovflw(nLTPQ14, lpcPredQ14) // Q13
			tmp1 = subSat32(tmp2, tmp1)             // Q13
			tmp1 = rshiftRound(tmp1, 4)             // Q10

			rQ10 := xQ10[i] - tmp1 // residual error Q10

			// Flip sign depending on dither.
			if psDD.seed < 0 {
				rQ10 = -rQ10
			}
			rQ10 = limit32(rQ10, -(31 << 10), 30<<10)

			// Find two quantization level candidates and measure their
			// rate-distortion.
			q1Q10 := rQ10 - int32(offsetQ10)
			q1Q0 := q1Q10 >> 10
			if lambdaQ10 > 2048 {
				// For aggressive RDO, the bias becomes more than one pulse.
				rdoOffset := int32(lambdaQ10/2 - 512)
				if q1Q10 > rdoOffset {
					q1Q0 = (q1Q10 - rdoOffset) >> 10
				} else if q1Q10 < -rdoOffset {
					q1Q0 = (q1Q10 + rdoOffset) >> 10
				} else if q1Q10 < 0 {
					q1Q0 = -1
				} else {
					q1Q0 = 0
				}
			}
			var q2Q10, rd1Q10, rd2Q10 int32
			if q1Q0 > 0 {
				q1Q10 = lshift(q1Q0, 10) - quantLevelAdjustQ10
				q1Q10 = q1Q10 + int32(offsetQ10)
				q2Q10 = q1Q10 + 1024
				rd1Q10 = smulbb(q1Q10, int32(lambdaQ10))
				rd2Q10 = smulbb(q2Q10, int32(lambdaQ10))
			} else if q1Q0 == 0 {
				q1Q10 = int32(offsetQ10)
				q2Q10 = q1Q10 + (1024 - quantLevelAdjustQ10)
				rd1Q10 = smulbb(q1Q10, int32(lambdaQ10))
				rd2Q10 = smulbb(q2Q10, int32(lambdaQ10))
			} else if q1Q0 == -1 {
				q2Q10 = int32(offsetQ10)
				q1Q10 = q2Q10 - (1024 - quantLevelAdjustQ10)
				rd1Q10 = smulbb(-q1Q10, int32(lambdaQ10))
				rd2Q10 = smulbb(q2Q10, int32(lambdaQ10))
			} else { // q1_Q0 < -1
				q1Q10 = lshift(q1Q0, 10) + quantLevelAdjustQ10
				q1Q10 = q1Q10 + int32(offsetQ10)
				q2Q10 = q1Q10 + 1024
				rd1Q10 = smulbb(-q1Q10, int32(lambdaQ10))
				rd2Q10 = smulbb(-q2Q10, int32(lambdaQ10))
			}
			rrQ10 := rQ10 - q1Q10
			rd1Q10 = smlabb(rd1Q10, rrQ10, rrQ10) >> 10
			rrQ10 = rQ10 - q2Q10
			rd2Q10 = smlabb(rd2Q10, rrQ10, rrQ10) >> 10

			if rd1Q10 < rd2Q10 {
				psSS[0].rdQ10 = psDD.rdQ10 + rd1Q10
				psSS[1].rdQ10 = psDD.rdQ10 + rd2Q10
				psSS[0].qQ10 = q1Q10
				psSS[1].qQ10 = q2Q10
			} else {
				psSS[0].rdQ10 = psDD.rdQ10 + rd2Q10
				psSS[1].rdQ10 = psDD.rdQ10 + rd1Q10
				psSS[0].qQ10 = q2Q10
				psSS[1].qQ10 = q1Q10
			}

			// Update states for best quantization.

			// Quantized excitation.
			excQ14 := lshift(psSS[0].qQ10, 4)
			if psDD.seed < 0 {
				excQ14 = -excQ14
			}

			// Add predictions.
			lpcExcQ14 := excQ14 + ltpPredQ14
			xqQ14 := add32ovflw(lpcExcQ14, lpcPredQ14)

			// Update states.
			psSS[0].diffQ14 = sub32ovflw(xqQ14, lshift(xQ10[i], 4))
			sLFARShpQ14 := sub32ovflw(psSS[0].diffQ14, nARQ14)
			psSS[0].sLTPShpQ14 = subSat32(sLFARShpQ14, nLFQ14)
			psSS[0].lfARQ14 = sLFARShpQ14
			psSS[0].lpcExcQ14 = lpcExcQ14
			psSS[0].xqQ14 = xqQ14

			// Update states for second best quantization.

			// Quantized excitation.
			excQ14 = lshift(psSS[1].qQ10, 4)
			if psDD.seed < 0 {
				excQ14 = -excQ14
			}

			// Add predictions.
			lpcExcQ14 = excQ14 + ltpPredQ14
			xqQ14 = add32ovflw(lpcExcQ14, lpcPredQ14)

			// Update states.
			psSS[1].diffQ14 = sub32ovflw(xqQ14, lshift(xQ10[i], 4))
			sLFARShpQ14 = sub32ovflw(psSS[1].diffQ14, nARQ14)
			psSS[1].sLTPShpQ14 = subSat32(sLFARShpQ14, nLFQ14)
			psSS[1].lfARQ14 = sLFARShpQ14
			psSS[1].lpcExcQ14 = lpcExcQ14
			psSS[1].xqQ14 = xqQ14
		}

		*smplBufIdx--
		if *smplBufIdx < 0 {
			*smplBufIdx += decisionDelay
		}
		lastSmpleIdx := *smplBufIdx + decDelay
		if lastSmpleIdx >= decisionDelay {
			lastSmpleIdx -= decisionDelay
		}

		// Find winner.
		rdMinQ10 := psSampleState[0][0].rdQ10
		winnerInd := 0
		for k := 1; k < nStatesDelayedDecision; k++ {
			if psSampleState[k][0].rdQ10 < rdMinQ10 {
				rdMinQ10 = psSampleState[k][0].rdQ10
				winnerInd = k
			}
		}

		// Increase RD values of expired states.
		winnerRandState := psDelDec[winnerInd].randState[lastSmpleIdx]
		for k := 0; k < nStatesDelayedDecision; k++ {
			if psDelDec[k].randState[lastSmpleIdx] != winnerRandState {
				psSampleState[k][0].rdQ10 = psSampleState[k][0].rdQ10 + int32MAX>>4
				psSampleState[k][1].rdQ10 = psSampleState[k][1].rdQ10 + int32MAX>>4
			}
		}

		// Find worst in first set and best in second set.
		rdMaxQ10 := psSampleState[0][0].rdQ10
		rdMinQ10 = psSampleState[0][1].rdQ10
		rdMaxInd := 0
		rdMinInd := 0
		for k := 1; k < nStatesDelayedDecision; k++ {
			// Find worst in first set.
			if psSampleState[k][0].rdQ10 > rdMaxQ10 {
				rdMaxQ10 = psSampleState[k][0].rdQ10
				rdMaxInd = k
			}
			// Find best in second set.
			if psSampleState[k][1].rdQ10 < rdMinQ10 {
				rdMinQ10 = psSampleState[k][1].rdQ10
				rdMinInd = k
			}
		}

		// Replace a state if best from second set outperforms worst in
		// first set.
		if rdMinQ10 < rdMaxQ10 {
			psDelDec[rdMaxInd].copyFrom(&psDelDec[rdMinInd], i)
			psSampleState[rdMaxInd][0] = psSampleState[rdMinInd][1]
		}

		// Write samples from winner to output and long-term filter states.
		psDD := &psDelDec[winnerInd]
		if subfr > 0 || i >= decDelay {
			pulses[pulsesOff+i-decDelay] = int8(rshiftRound(psDD.qQ10[lastSmpleIdx], 10))
			nsq.xq[xqOff+i-decDelay] = int16(sat16(rshiftRound(
				smulww(psDD.xqQ14[lastSmpleIdx], delayedGainQ10[lastSmpleIdx]), 8)))
			nsq.sLTPShpQ14[nsq.sLTPShpBufIdx-decDelay] = psDD.shapeQ14[lastSmpleIdx]
			sLTPQ15[nsq.sLTPBufIdx-decDelay] = psDD.predQ15[lastSmpleIdx]
		}
		nsq.sLTPShpBufIdx++
		nsq.sLTPBufIdx++

		// Update states.
		for k := 0; k < nStatesDelayedDecision; k++ {
			psDD := &psDelDec[k]
			psSS := &psSampleState[k][0]
			psDD.lfARQ14 = psSS.lfARQ14
			psDD.diffQ14 = psSS.diffQ14
			psDD.sLPCQ14[nsqLPCBufLength+i] = psSS.xqQ14
			psDD.xqQ14[*smplBufIdx] = psSS.xqQ14
			psDD.qQ10[*smplBufIdx] = psSS.qQ10
			psDD.predQ15[*smplBufIdx] = lshift(psSS.lpcExcQ14, 1)
			psDD.shapeQ14[*smplBufIdx] = psSS.sLTPShpQ14
			psDD.seed = add32ovflw(psDD.seed, rshiftRound(psSS.qQ10, 10))
			psDD.randState[*smplBufIdx] = psDD.seed
			psDD.rdQ10 = psSS.rdQ10
		}
		delayedGainQ10[*smplBufIdx] = gainQ10
	}
	// Update LPC states.
	for k := 0; k < nStatesDelayedDecision; k++ {
		psDD := &psDelDec[k]
		copy(psDD.sLPCQ14[:nsqLPCBufLength], psDD.sLPCQ14[length:length+nsqLPCBufLength])
	}
}

// nsqDelDecScaleStates is silk_nsq_del_dec_scale_states.
// preserve the reference prediction, gain and numerical decision sequence
//
//revive:disable-next-line:cognitive-complexity
func (psEncC *encoderState) nsqDelDecScaleStates(
	nsq *nsqState,
	psDelDec []nsqDelDecStruct,
	x16 []int16,
	xScQ10 []int32,
	sLTP []int16,
	sLTPQ15 []int32,
	subfr, nStatesDelayedDecision, ltpScaleQ14 int,
	gainsQ16 *[maxNbSubfr]int32,
	pitchL *[maxNbSubfr]int,
	signalType, decDelay int,
) {
	lag := pitchL[subfr]
	invGainQ31 := inverse32VarQ(max32(gainsQ16[subfr], 1), 47)

	// Scale input.
	invGainQ26 := rshiftRound(invGainQ31, 5)
	for i := 0; i < psEncC.subfrLength; i++ {
		xScQ10[i] = smulww(int32(x16[i]), invGainQ26)
	}

	// After rewhitening the LTP state is un-scaled, so scale with
	// inv_gain_Q16.
	if nsq.rewhiteFlag != 0 {
		if subfr == 0 {
			// Do LTP downscaling.
			invGainQ31 = lshift(smulwb(invGainQ31, int32(ltpScaleQ14)), 2)
		}
		for i := nsq.sLTPBufIdx - lag - ltpOrder/2; i < nsq.sLTPBufIdx; i++ {
			sLTPQ15[i] = smulwb(invGainQ31, int32(sLTP[i]))
		}
	}

	// Adjust for changing gain.
	if gainsQ16[subfr] != nsq.prevGainQ16 {
		gainAdjQ16 := div32VarQ(nsq.prevGainQ16, gainsQ16[subfr], 16)

		// Scale long-term shaping state.
		for i := nsq.sLTPShpBufIdx - psEncC.ltpMemLength; i < nsq.sLTPShpBufIdx; i++ {
			nsq.sLTPShpQ14[i] = smulww(gainAdjQ16, nsq.sLTPShpQ14[i])
		}

		// Scale long-term prediction state.
		if signalType == typeVoiced && nsq.rewhiteFlag == 0 {
			for i := nsq.sLTPBufIdx - lag - ltpOrder/2; i < nsq.sLTPBufIdx-decDelay; i++ {
				sLTPQ15[i] = smulww(gainAdjQ16, sLTPQ15[i])
			}
		}

		for k := 0; k < nStatesDelayedDecision; k++ {
			psDD := &psDelDec[k]

			// Scale scalar states.
			psDD.lfARQ14 = smulww(gainAdjQ16, psDD.lfARQ14)
			psDD.diffQ14 = smulww(gainAdjQ16, psDD.diffQ14)

			// Scale short-term prediction and shaping states.
			for i := 0; i < nsqLPCBufLength; i++ {
				psDD.sLPCQ14[i] = smulww(gainAdjQ16, psDD.sLPCQ14[i])
			}
			for i := 0; i < maxShapeLPCOrder; i++ {
				psDD.sAR2Q14[i] = smulww(gainAdjQ16, psDD.sAR2Q14[i])
			}
			for i := 0; i < decisionDelay; i++ {
				psDD.predQ15[i] = smulww(gainAdjQ16, psDD.predQ15[i])
				psDD.shapeQ14[i] = smulww(gainAdjQ16, psDD.shapeQ14[i])
			}
		}

		// Save inverse gain.
		nsq.prevGainQ16 = gainsQ16[subfr]
	}
}

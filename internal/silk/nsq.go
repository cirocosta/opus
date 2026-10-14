// Ported from libopus 1.6.1 silk/NSQ.c and silk/NSQ.h.

package silk

import "github.com/cirocosta/opus/internal/kernels"

// nsqScratch holds the buffers that silk_NSQ and silk_NSQ_del_dec
// allocate on the stack (ALLOC). They carry no state between calls.
type nsqScratch struct {
	sLTPQ15        [2 * maxFrameLength]int32
	sLTP           [2 * maxFrameLength]int16
	xScQ10         [maxSubFrameLength]int32
	delayedGainQ10 [decisionDelay]int32
	psDelDec       [maxDelDecStates]nsqDelDecStruct
	psSampleState  [maxDelDecStates][2]nsqSampleStruct
}

// nsqParams holds the per-frame NSQ inputs that the FLP wrapper
// converts to fixed point (the arguments of silk_NSQ after pulses).
type nsqParams struct {
	predCoefQ12      [2][maxLPCOrder]int16
	ltpCoefQ14       [ltpOrder * maxNbSubfr]int16
	arQ13            [maxNbSubfr * maxShapeLPCOrder]int16
	harmShapeGainQ14 [maxNbSubfr]int
	tiltQ14          [maxNbSubfr]int
	lfShpQ14         [maxNbSubfr]int32
	gainsQ16         [maxNbSubfr]int32
	pitchL           [maxNbSubfr]int
	lambdaQ10        int
	ltpScaleQ14      int
}

// noiseShapeQuantizerShortPrediction is
// silk_noise_shape_quantizer_short_prediction_c. buf32[p-j] is
// buf32[ -j ] in C.
func noiseShapeQuantizerShortPrediction(buf32 []int32, p int, coef16 []int16, order int) int32 {
	return kernels.Predict(buf32[:p+1], coef16, order)
}

// nsqNoiseShapeFeedbackLoop is silk_NSQ_noise_shape_feedback_loop_c.
func nsqNoiseShapeFeedbackLoop(data0 *int32, data1 []int32, coef []int16, order int) int32 {
	tmp2 := *data0
	tmp1 := data1[0]
	data1[0] = tmp2

	out := int32(order >> 1)
	out = smlawb(out, tmp2, int32(coef[0]))

	for j := 2; j < order; j += 2 {
		tmp2 = data1[j-1]
		data1[j-1] = tmp1
		out = smlawb(out, tmp1, int32(coef[j-1]))
		tmp1 = data1[j+0]
		data1[j+0] = tmp2
		out = smlawb(out, tmp2, int32(coef[j]))
	}
	data1[order-1] = tmp1
	out = smlawb(out, tmp1, int32(coef[order-1]))
	// Q11 -> Q12.
	out = lshift(out, 1)
	return out
}

// nsq is silk_NSQ (silk_NSQ_c): noise shaping quantization.
func (psEncC *encoderState) nsq(
	nsq *nsqState,
	psIndices *sideInfoIndices,
	x16 []int16,
	pulses []int8,
	p *nsqParams,
	sc *nsqScratch,
) {
	nsq.randSeed = int32(psIndices.seed)

	// Set unvoiced lag to the previous one, overwrite later for voiced.
	lag := nsq.lagPrev

	offsetQ10 := int(quantizationOffsetsQ10[psIndices.signalType>>1][psIndices.quantOffsetType])

	lsfInterpolationFlag := 1
	if psIndices.nlsfInterpCoefQ2 == 4 {
		lsfInterpolationFlag = 0
	}

	sLTPQ15 := sc.sLTPQ15[:psEncC.ltpMemLength+psEncC.frameLength]
	sLTP := sc.sLTP[:psEncC.ltpMemLength+psEncC.frameLength]
	xScQ10 := sc.xScQ10[:psEncC.subfrLength]
	// Set up pointers to start of sub frame.
	nsq.sLTPShpBufIdx = psEncC.ltpMemLength
	nsq.sLTPBufIdx = psEncC.ltpMemLength
	pxq := psEncC.ltpMemLength
	x16Off := 0
	pulsesOff := 0
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
				// Rewhiten with new A coefs.
				startIdx := psEncC.ltpMemLength - lag - psEncC.predictLPCOrder - ltpOrder/2

				lpcAnalysisFilter(sLTP[startIdx:], nsq.xq[startIdx+k*psEncC.subfrLength:],
					aQ12, psEncC.ltpMemLength-startIdx, psEncC.predictLPCOrder)

				nsq.rewhiteFlag = 1
				nsq.sLTPBufIdx = psEncC.ltpMemLength
			}
		}

		psEncC.nsqScaleStates(
			nsq,
			x16[x16Off:],
			xScQ10,
			sLTP,
			sLTPQ15,
			k,
			p.ltpScaleQ14,
			&p.gainsQ16,
			&p.pitchL,
			int(psIndices.signalType),
		)

		nsq.noiseShapeQuantizer(
			int(psIndices.signalType),
			xScQ10,
			pulses[pulsesOff:],
			nsq.xq[pxq:],
			sLTPQ15,
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
			psEncC.shapingLPCOrder,
			psEncC.predictLPCOrder,
		)

		x16Off += psEncC.subfrLength
		pulsesOff += psEncC.subfrLength
		pxq += psEncC.subfrLength
	}

	// Update lagPrev for next frame.
	nsq.lagPrev = p.pitchL[psEncC.nbSubfr-1]

	// Save quantized speech and noise shaping signals.
	copy(
		nsq.xq[:psEncC.ltpMemLength],
		nsq.xq[psEncC.frameLength:psEncC.frameLength+psEncC.ltpMemLength],
	)
	copy(
		nsq.sLTPShpQ14[:psEncC.ltpMemLength],
		nsq.sLTPShpQ14[psEncC.frameLength:psEncC.frameLength+psEncC.ltpMemLength],
	)
}

// noiseShapeQuantizer is silk_noise_shape_quantizer.
// preserve the reference prediction, gain and numerical decision sequence
//
//revive:disable-next-line:cognitive-complexity
func (nsq *nsqState) noiseShapeQuantizer(
	signalType int,
	xScQ10 []int32,
	pulses []int8,
	xq []int16,
	sLTPQ15 []int32,
	aQ12, bQ14, arShpQ13 []int16,
	lag int,
	harmShapeFIRPackedQ14 int32,
	tiltQ14 int,
	lfShpQ14, gainQ16 int32,
	lambdaQ10, offsetQ10, length, shapingLPCOrder, predictLPCOrder int,
) {
	shpLag := nsq.sLTPShpBufIdx - lag + harmShapeFIRTaps/2
	predLag := nsq.sLTPBufIdx - lag + ltpOrder/2
	gainQ10 := gainQ16 >> 6

	// Set up short term AR state.
	psLPCQ14 := nsqLPCBufLength - 1

	for i := 0; i < length; i++ {
		// Generate dither.
		nsq.randSeed = silkRAND(nsq.randSeed)

		// Short-term prediction.
		lpcPredQ10 := noiseShapeQuantizerShortPrediction(
			nsq.sLPCQ14[:],
			psLPCQ14,
			aQ12,
			predictLPCOrder,
		)

		// Long-term prediction.
		var ltpPredQ13 int32
		if signalType == typeVoiced {
			// Unrolled loop.
			// Avoids introducing a bias because silk_SMLAWB() always rounds
			// to -inf.
			ltpPredQ13 = 2
			ltpPredQ13 = smlawb(ltpPredQ13, sLTPQ15[predLag-0], int32(bQ14[0]))
			ltpPredQ13 = smlawb(ltpPredQ13, sLTPQ15[predLag-1], int32(bQ14[1]))
			ltpPredQ13 = smlawb(ltpPredQ13, sLTPQ15[predLag-2], int32(bQ14[2]))
			ltpPredQ13 = smlawb(ltpPredQ13, sLTPQ15[predLag-3], int32(bQ14[3]))
			ltpPredQ13 = smlawb(ltpPredQ13, sLTPQ15[predLag-4], int32(bQ14[4]))
			predLag++
		} else {
			ltpPredQ13 = 0
		}

		// Noise shape feedback.
		nARQ12 := nsqNoiseShapeFeedbackLoop(
			&nsq.sDiffShpQ14,
			nsq.sAR2Q14[:],
			arShpQ13,
			shapingLPCOrder,
		)

		nARQ12 = smlawb(nARQ12, nsq.sLFARShpQ14, int32(tiltQ14))

		nLFQ12 := smulwb(nsq.sLTPShpQ14[nsq.sLTPShpBufIdx-1], lfShpQ14)
		nLFQ12 = smlawt(nLFQ12, nsq.sLFARShpQ14, lfShpQ14)

		// Combine prediction and noise shaping signals.
		tmp1 := sub32ovflw(lshift(lpcPredQ10, 2), nARQ12) // Q12
		tmp1 = sub32ovflw(tmp1, nLFQ12)                   // Q12
		if lag > 0 {
			// Symmetric, packed FIR coefficients.
			nLTPQ13 := smulwb(
				addSat32(nsq.sLTPShpQ14[shpLag-0], nsq.sLTPShpQ14[shpLag-2]),
				harmShapeFIRPackedQ14,
			)
			nLTPQ13 = smlawt(nLTPQ13, nsq.sLTPShpQ14[shpLag-1], harmShapeFIRPackedQ14)
			nLTPQ13 = lshift(nLTPQ13, 1)
			shpLag++

			tmp2 := ltpPredQ13 - nLTPQ13             // Q13
			tmp1 = add32ovflw(tmp2, lshift(tmp1, 1)) // Q13
			tmp1 = rshiftRound(tmp1, 3)              // Q10
		} else {
			tmp1 = rshiftRound(tmp1, 2) // Q10
		}

		rQ10 := xScQ10[i] - tmp1 // residual error Q10

		// Flip sign depending on dither.
		if nsq.randSeed < 0 {
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
		var q2Q10, rd1Q20, rd2Q20 int32
		if q1Q0 > 0 {
			q1Q10 = lshift(q1Q0, 10) - quantLevelAdjustQ10
			q1Q10 = q1Q10 + int32(offsetQ10)
			q2Q10 = q1Q10 + 1024
			rd1Q20 = smulbb(q1Q10, int32(lambdaQ10))
			rd2Q20 = smulbb(q2Q10, int32(lambdaQ10))
		} else if q1Q0 == 0 {
			q1Q10 = int32(offsetQ10)
			q2Q10 = q1Q10 + (1024 - quantLevelAdjustQ10)
			rd1Q20 = smulbb(q1Q10, int32(lambdaQ10))
			rd2Q20 = smulbb(q2Q10, int32(lambdaQ10))
		} else if q1Q0 == -1 {
			q2Q10 = int32(offsetQ10)
			q1Q10 = q2Q10 - (1024 - quantLevelAdjustQ10)
			rd1Q20 = smulbb(-q1Q10, int32(lambdaQ10))
			rd2Q20 = smulbb(q2Q10, int32(lambdaQ10))
		} else { // Q1_Q0 < -1
			q1Q10 = lshift(q1Q0, 10) + quantLevelAdjustQ10
			q1Q10 = q1Q10 + int32(offsetQ10)
			q2Q10 = q1Q10 + 1024
			rd1Q20 = smulbb(-q1Q10, int32(lambdaQ10))
			rd2Q20 = smulbb(-q2Q10, int32(lambdaQ10))
		}
		rrQ10 := rQ10 - q1Q10
		rd1Q20 = smlabb(rd1Q20, rrQ10, rrQ10)
		rrQ10 = rQ10 - q2Q10
		rd2Q20 = smlabb(rd2Q20, rrQ10, rrQ10)

		if rd2Q20 < rd1Q20 {
			q1Q10 = q2Q10
		}

		pulses[i] = int8(rshiftRound(q1Q10, 10))

		// Excitation.
		excQ14 := lshift(q1Q10, 4)
		if nsq.randSeed < 0 {
			excQ14 = -excQ14
		}

		// Add predictions.
		lpcExcQ14 := addLshift32(excQ14, ltpPredQ13, 1)
		xqQ14 := add32ovflw(lpcExcQ14, lshift(lpcPredQ10, 4))

		// Scale XQ back to normal level before saving.
		xq[i] = int16(sat16(rshiftRound(smulww(xqQ14, gainQ10), 8)))

		// Update states.
		psLPCQ14++
		nsq.sLPCQ14[psLPCQ14] = xqQ14
		nsq.sDiffShpQ14 = sub32ovflw(xqQ14, lshift(xScQ10[i], 4))
		sLFARShpQ14 := sub32ovflw(nsq.sDiffShpQ14, lshift(nARQ12, 2))
		nsq.sLFARShpQ14 = sLFARShpQ14

		nsq.sLTPShpQ14[nsq.sLTPShpBufIdx] = sub32ovflw(sLFARShpQ14, lshift(nLFQ12, 2))
		sLTPQ15[nsq.sLTPBufIdx] = lshift(lpcExcQ14, 1)
		nsq.sLTPShpBufIdx++
		nsq.sLTPBufIdx++

		// Make dither dependent on quantized signal.
		nsq.randSeed = add32ovflw(nsq.randSeed, int32(pulses[i]))
	}

	// Update LPC synth buffer.
	copy(nsq.sLPCQ14[:nsqLPCBufLength], nsq.sLPCQ14[length:length+nsqLPCBufLength])
}

// nsqScaleStates is silk_nsq_scale_states.
// preserve the reference prediction, gain and numerical decision sequence
//
//revive:disable-next-line:cognitive-complexity
func (psEncC *encoderState) nsqScaleStates(
	nsq *nsqState,
	x16 []int16,
	xScQ10 []int32,
	sLTP []int16,
	sLTPQ15 []int32,
	subfr, ltpScaleQ14 int,
	gainsQ16 *[maxNbSubfr]int32,
	pitchL *[maxNbSubfr]int,
	signalType int,
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
			for i := nsq.sLTPBufIdx - lag - ltpOrder/2; i < nsq.sLTPBufIdx; i++ {
				sLTPQ15[i] = smulww(gainAdjQ16, sLTPQ15[i])
			}
		}

		nsq.sLFARShpQ14 = smulww(gainAdjQ16, nsq.sLFARShpQ14)
		nsq.sDiffShpQ14 = smulww(gainAdjQ16, nsq.sDiffShpQ14)

		// Scale short-term prediction and shaping states.
		for i := 0; i < nsqLPCBufLength; i++ {
			nsq.sLPCQ14[i] = smulww(gainAdjQ16, nsq.sLPCQ14[i])
		}
		for i := 0; i < maxShapeLPCOrder; i++ {
			nsq.sAR2Q14[i] = smulww(gainAdjQ16, nsq.sAR2Q14[i])
		}

		// Save inverse gain.
		nsq.prevGainQ16 = gainsQ16[subfr]
	}
}

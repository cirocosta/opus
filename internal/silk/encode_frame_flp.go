// Ported from libopus 1.6.1 silk/float/encode_frame_FLP.c.

package silk

import "github.com/cirocosta/opus/internal/entcode"

// Constants of silk_encode_frame_FLP.
var (
	vadActivityThresholdQ8 = int(
		fixConstF32(speechActivityDTXThres, 8),
	) // SILK_FIX_CONST( SPEECH_ACTIVITY_DTX_THRES, 8 )
	lbrrSpeechActThresQ8 = int(
		fixConstF32(lbrrSpeechActivityThres, 8),
	) // SILK_FIX_CONST( LBRR_SPEECH_ACTIVITY_THRES, 8 )
)

// encodeDoVADFLP is silk_encode_do_VAD_FLP. activity is the decision of
// the Opus voice activity detector.
func (psEnc *encoderStateFLP) encodeDoVADFLP(activity int) {
	activityThreshold := vadActivityThresholdQ8

	// Voice Activity Detection.
	psEnc.sCmn.vadGetSAQ8(psEnc.sCmn.inputBuf[1:])
	// If Opus VAD is inactive and Silk VAD is active: lower Silk VAD to
	// just under the threshold.
	if activity == vadNoActivity && psEnc.sCmn.speechActivityQ8 >= activityThreshold {
		psEnc.sCmn.speechActivityQ8 = activityThreshold - 1
	}

	// Convert speech activity into VAD and DTX flags.
	if psEnc.sCmn.speechActivityQ8 < activityThreshold {
		psEnc.sCmn.indices.signalType = typeNoVoiceActivity
		psEnc.sCmn.noSpeechCounter++
		if psEnc.sCmn.noSpeechCounter <= nbSpeechFramesBeforeDTX {
			psEnc.sCmn.inDTX = 0
		} else if psEnc.sCmn.noSpeechCounter > maxConsecutiveDTX+nbSpeechFramesBeforeDTX {
			psEnc.sCmn.noSpeechCounter = nbSpeechFramesBeforeDTX
			psEnc.sCmn.inDTX = 0
		}
		psEnc.sCmn.vadFlags[psEnc.sCmn.nFramesEncoded] = 0
	} else {
		psEnc.sCmn.noSpeechCounter = 0
		psEnc.sCmn.inDTX = 0
		psEnc.sCmn.indices.signalType = typeUnvoiced
		psEnc.sCmn.vadFlags[psEnc.sCmn.nFramesEncoded] = 1
	}
}

// encodeFrameFLP is silk_encode_frame_FLP: encode a frame.
// preserve the reference prediction, gain and numerical decision sequence
//
//revive:disable-next-line:cognitive-complexity
func (psEnc *encoderStateFLP) encodeFrameFLP(
	pnBytesOut *int32,
	psRangeEnc *entcode.Encoder,
	condCoding, maxBits, useCBR int,
) int {
	var (
		sRangeEncCopy, sRangeEncCopy2 entcode.Encoder
		pGainsQ16                     [maxNbSubfr]int32
		gainLock                      [maxNbSubfr]int
		bestGainMult                  [maxNbSubfr]int16
		bestSum                       [maxNbSubfr]int
		nBits                         int32
	)
	ret := 0
	sc := &psEnc.scratch
	sEncCtrl := &sc.sEncCtrl
	*sEncCtrl = encoderControlFLP{}
	resPitch := sc.resPitch[:]
	sNSQCopy := &sc.sNSQCopy

	// For CBR, 5 bits below budget is close enough. For VBR, allow up to
	// 25% below the cap if we initially busted the budget.
	bitsMargin := maxBits / 4
	if useCBR != 0 {
		bitsMargin = 5
	}
	var lastGainIndexCopy2 int8
	var nBitsLower, nBitsUpper, gainMultLower, gainMultUpper int32

	psEnc.sCmn.indices.seed = int8(psEnc.sCmn.frameCounter & 3)
	psEnc.sCmn.frameCounter++

	// Set up Input Pointers, and insert frame in input buffer.
	// Pointers aligned with start of frame to encode.
	xFrame := psEnc.sCmn.ltpMemLength        // start of frame to encode
	resPitchFrame := psEnc.sCmn.ltpMemLength // start of pitch LPC residual frame

	// Ensure smooth bandwidth transitions.
	lpVariableCutoff(&psEnc.sCmn.sLP, psEnc.sCmn.inputBuf[1:], psEnc.sCmn.frameLength)

	// Copy new frame to front of input buffer.
	short2floatArray(
		psEnc.xBuf[xFrame+laShapeMs*psEnc.sCmn.fsKHz:],
		psEnc.sCmn.inputBuf[1:],
		psEnc.sCmn.frameLength,
	)

	// Add tiny signal to avoid high CPU load from denormalized floating
	// point numbers.
	for i := 0; i < 8; i++ {
		psEnc.xBuf[xFrame+laShapeMs*psEnc.sCmn.fsKHz+i*(psEnc.sCmn.frameLength>>3)] += float32(
			float32(1-(i&2)) * 1e-6,
		)
	}

	if psEnc.sCmn.prefillFlag == 0 {
		// Find pitch lags, initial LPC analysis.
		psEnc.findPitchLagsFLP(sEncCtrl, resPitch, psEnc.xBuf[:], xFrame)

		// Noise shape analysis.
		psEnc.noiseShapeAnalysisFLP(sEncCtrl, resPitch, resPitchFrame, psEnc.xBuf[:], xFrame)

		// Find linear prediction coefficients (LPC + LTP).
		psEnc.findPredCoefsFLP(sEncCtrl, resPitch, resPitchFrame, psEnc.xBuf[:], xFrame, condCoding)

		// Process gains.
		psEnc.processGainsFLP(sEncCtrl, condCoding)

		// Low Bitrate Redundant Encoding.
		psEnc.lbrrEncodeFLP(sEncCtrl, psEnc.xBuf[:], xFrame, condCoding)

		// Loop over quantizer and entroy coding to control bitrate.
		maxIter := 6
		gainMultQ8 := int16(1 << 8)
		foundLower := 0
		foundUpper := 0
		gainsIDv := gainsID(&psEnc.sCmn.indices.gainsIndices, psEnc.sCmn.nbSubfr)
		gainsIDLower := int32(-1)
		gainsIDUpper := int32(-1)
		// Copy part of the input state.
		sRangeEncCopy = *psRangeEnc
		sNSQCopy[0] = psEnc.sCmn.sNSQ
		seedCopy := psEnc.sCmn.indices.seed
		ecPrevLagIndexCopy := psEnc.sCmn.ecPrevLagIndex
		ecPrevSignalTypeCopy := psEnc.sCmn.ecPrevSignalType
		ecBufCopy := sc.ecBufCopy[:]
	rateSearch:
		for iter := 0; ; iter++ {
			switch gainsIDv {
			case gainsIDLower:
				nBits = nBitsLower
			case gainsIDUpper:
				nBits = nBitsUpper
			default:
				// Restore part of the input state.
				if iter > 0 {
					*psRangeEnc = sRangeEncCopy
					psEnc.sCmn.sNSQ = sNSQCopy[0]
					psEnc.sCmn.indices.seed = seedCopy
					psEnc.sCmn.ecPrevLagIndex = ecPrevLagIndexCopy
					psEnc.sCmn.ecPrevSignalType = ecPrevSignalTypeCopy
				}

				// Noise shaping quantization.
				psEnc.nsqWrapperFLP(
					sEncCtrl,
					&psEnc.sCmn.indices,
					&psEnc.sCmn.sNSQ,
					psEnc.sCmn.pulses[:],
					psEnc.xBuf[:],
					xFrame,
				)

				if iter == maxIter && foundLower == 0 {
					sRangeEncCopy2 = *psRangeEnc
				}

				// Encode Parameters.
				psEnc.sCmn.encodeIndices(psRangeEnc, psEnc.sCmn.nFramesEncoded, false, condCoding)

				// Encode Excitation Signal.
				encodePulses(
					psRangeEnc,
					int(psEnc.sCmn.indices.signalType),
					int(psEnc.sCmn.indices.quantOffsetType),
					psEnc.sCmn.pulses[:],
					psEnc.sCmn.frameLength,
				)

				nBits = int32(psRangeEnc.Tell())

				// If we still bust after the last iteration, do some damage
				// control.
				if iter == maxIter && foundLower == 0 && nBits > int32(maxBits) {
					*psRangeEnc = sRangeEncCopy2

					// Keep gains the same as the last frame.
					psEnc.sShape.lastGainIndex = sEncCtrl.lastGainIndexPrev
					for i := 0; i < psEnc.sCmn.nbSubfr; i++ {
						psEnc.sCmn.indices.gainsIndices[i] = 4
					}
					if condCoding != codeConditionally {
						psEnc.sCmn.indices.gainsIndices[0] = sEncCtrl.lastGainIndexPrev
					}
					psEnc.sCmn.ecPrevLagIndex = ecPrevLagIndexCopy
					psEnc.sCmn.ecPrevSignalType = ecPrevSignalTypeCopy
					// Clear all pulses.
					for i := 0; i < psEnc.sCmn.frameLength; i++ {
						psEnc.sCmn.pulses[i] = 0
					}

					psEnc.sCmn.encodeIndices(
						psRangeEnc,
						psEnc.sCmn.nFramesEncoded,
						false,
						condCoding,
					)

					encodePulses(
						psRangeEnc,
						int(psEnc.sCmn.indices.signalType),
						int(psEnc.sCmn.indices.quantOffsetType),
						psEnc.sCmn.pulses[:],
						psEnc.sCmn.frameLength,
					)

					nBits = int32(psRangeEnc.Tell())
				}

				if useCBR == 0 && iter == 0 && nBits <= int32(maxBits) {
					break rateSearch
				}
			}

			if iter == maxIter {
				if foundLower != 0 && (gainsIDv == gainsIDLower || nBits > int32(maxBits)) {
					// Restore output state from earlier iteration that did
					// meet the bitrate budget.
					*psRangeEnc = sRangeEncCopy2
					copy(psRangeEnc.Buf[:sRangeEncCopy2.Offs], ecBufCopy[:sRangeEncCopy2.Offs])
					psEnc.sCmn.sNSQ = sNSQCopy[1]
					psEnc.sShape.lastGainIndex = lastGainIndexCopy2
				}
				break rateSearch
			}

			if nBits > int32(maxBits) {
				if foundLower == 0 && iter >= 2 {
					// Adjust the quantizer's rate/distortion tradeoff and
					// discard previous "upper" results.
					sEncCtrl.lambda = maxFloat(float32(sEncCtrl.lambda*1.5), 1.5)
					// Reducing dithering can help us hit the target.
					psEnc.sCmn.indices.quantOffsetType = 0
					foundUpper = 0
					gainsIDUpper = -1
				} else {
					foundUpper = 1
					nBitsUpper = nBits
					gainMultUpper = int32(gainMultQ8)
					gainsIDUpper = gainsIDv
				}
			} else if nBits < int32(maxBits-bitsMargin) {
				foundLower = 1
				nBitsLower = nBits
				gainMultLower = int32(gainMultQ8)
				if gainsIDv != gainsIDLower {
					gainsIDLower = gainsIDv
					// Copy part of the output state.
					sRangeEncCopy2 = *psRangeEnc
					copy(ecBufCopy[:psRangeEnc.Offs], psRangeEnc.Buf[:psRangeEnc.Offs])
					sNSQCopy[1] = psEnc.sCmn.sNSQ
					lastGainIndexCopy2 = psEnc.sShape.lastGainIndex
				}
			} else {
				// Close enough.
				break rateSearch
			}

			if foundLower == 0 && nBits > int32(maxBits) {
				for i := 0; i < psEnc.sCmn.nbSubfr; i++ {
					sum := 0
					for j := i * psEnc.sCmn.subfrLength; j < (i+1)*psEnc.sCmn.subfrLength; j++ {
						sum += absInt(int(psEnc.sCmn.pulses[j]))
					}
					if iter == 0 || (sum < bestSum[i] && gainLock[i] == 0) {
						bestSum[i] = sum
						bestGainMult[i] = gainMultQ8
					} else {
						gainLock[i] = 1
					}
				}
			}
			if (foundLower & foundUpper) == 0 {
				// Adjust gain according to high-rate rate/distortion curve.
				if nBits > int32(maxBits) {
					gainMultQ8 = int16(min32(1024, int32(gainMultQ8)*3/2))
				} else {
					gainMultQ8 = int16(max32(64, int32(gainMultQ8)*4/5))
				}
			} else {
				// Adjust gain by interpolating.
				gainMultQ8 = int16(
					gainMultLower + ((gainMultUpper-gainMultLower)*(int32(maxBits)-nBitsLower))/(nBitsUpper-nBitsLower),
				)
				// New gain multiplier must be between 25% and 75% of old range
				// (note that gainMult_upper < gainMult_lower).
				if int32(gainMultQ8) > addRshift32(gainMultLower, gainMultUpper-gainMultLower, 2) {
					gainMultQ8 = int16(addRshift32(gainMultLower, gainMultUpper-gainMultLower, 2))
				} else if int32(gainMultQ8) < subRshift32(gainMultUpper, gainMultUpper-gainMultLower, 2) {
					gainMultQ8 = int16(subRshift32(gainMultUpper, gainMultUpper-gainMultLower, 2))
				}
			}

			for i := 0; i < psEnc.sCmn.nbSubfr; i++ {
				var tmp int16
				if gainLock[i] != 0 {
					tmp = bestGainMult[i]
				} else {
					tmp = gainMultQ8
				}
				pGainsQ16[i] = lshiftSat32(smulwb(sEncCtrl.gainsUnqQ16[i], int32(tmp)), 8)
			}

			// Quantize gains.
			psEnc.sShape.lastGainIndex = sEncCtrl.lastGainIndexPrev
			gainsQuant(&psEnc.sCmn.indices.gainsIndices, &pGainsQ16,
				&psEnc.sShape.lastGainIndex, condCoding == codeConditionally, psEnc.sCmn.nbSubfr)

			// Unique identifier of gains vector.
			gainsIDv = gainsID(&psEnc.sCmn.indices.gainsIndices, psEnc.sCmn.nbSubfr)

			// Overwrite unquantized gains with quantized gains and convert
			// back to Q0 from Q16.
			for i := 0; i < psEnc.sCmn.nbSubfr; i++ {
				sEncCtrl.gains[i] = float32(pGainsQ16[i]) / 65536.0
			}
		}
	}

	// Update input buffer.
	n := psEnc.sCmn.ltpMemLength + laShapeMs*psEnc.sCmn.fsKHz
	copy(psEnc.xBuf[:n], psEnc.xBuf[psEnc.sCmn.frameLength:psEnc.sCmn.frameLength+n])

	// Exit without entropy coding.
	if psEnc.sCmn.prefillFlag != 0 {
		// No payload.
		*pnBytesOut = 0
		return ret
	}

	// Parameters needed for next frame.
	psEnc.sCmn.prevLag = sEncCtrl.pitchL[psEnc.sCmn.nbSubfr-1]
	psEnc.sCmn.prevSignalType = psEnc.sCmn.indices.signalType

	// Finalize payload.
	psEnc.sCmn.firstFrameAfterReset = 0
	// Payload size.
	*pnBytesOut = int32((psRangeEnc.Tell() + 7) >> 3)

	return ret
}

// lbrrEncodeFLP is silk_LBRR_encode_FLP: Low-Bitrate Redundancy (LBRR)
// encoding. Reuse all parameters but encode excitation at lower
// bitrate. xfw[xOff] is the C xfw pointer.
func (psEnc *encoderStateFLP) lbrrEncodeFLP(
	psEncCtrl *encoderControlFLP,
	xfw []float32,
	xOff int,
	condCoding int,
) {
	var (
		gainsQ16  [maxNbSubfr]int32
		tempGains [maxNbSubfr]float32
	)
	psIndicesLBRR := &psEnc.sCmn.indicesLBRR[psEnc.sCmn.nFramesEncoded]

	// Control use of inband LBRR.
	if psEnc.sCmn.lbrrEnabled != 0 && psEnc.sCmn.speechActivityQ8 > lbrrSpeechActThresQ8 {
		sNSQLBRR := &psEnc.scratch.sNSQLBRR

		psEnc.sCmn.lbrrFlags[psEnc.sCmn.nFramesEncoded] = 1

		// Copy noise shaping quantizer state and quantization indices from
		// regular encoding.
		*sNSQLBRR = psEnc.sCmn.sNSQ
		*psIndicesLBRR = psEnc.sCmn.indices

		// Save original gains.
		copy(tempGains[:psEnc.sCmn.nbSubfr], psEncCtrl.gains[:psEnc.sCmn.nbSubfr])

		if psEnc.sCmn.nFramesEncoded == 0 ||
			psEnc.sCmn.lbrrFlags[psEnc.sCmn.nFramesEncoded-1] == 0 {
			// First frame in packet or previous frame not LBRR coded.
			psEnc.sCmn.lbrrPrevLastGainIndex = psEnc.sShape.lastGainIndex

			// Increase Gains to get target LBRR rate.
			psIndicesLBRR.gainsIndices[0] = int8(
				int(psIndicesLBRR.gainsIndices[0]) + psEnc.sCmn.lbrrGainIncreases,
			)
			psIndicesLBRR.gainsIndices[0] = int8(
				minInt(int(psIndicesLBRR.gainsIndices[0]), nLevelsQGain-1),
			)
		}

		// Decode to get gains in sync with decoder.
		gainsDequant(&gainsQ16, &psIndicesLBRR.gainsIndices,
			&psEnc.sCmn.lbrrPrevLastGainIndex, condCoding == codeConditionally, psEnc.sCmn.nbSubfr)

		// Overwrite unquantized gains with quantized gains and convert back
		// to Q0 from Q16.
		for k := 0; k < psEnc.sCmn.nbSubfr; k++ {
			psEncCtrl.gains[k] = float32(float32(gainsQ16[k]) * (1.0 / 65536.0))
		}

		// Noise shaping quantization.
		psEnc.nsqWrapperFLP(psEncCtrl, psIndicesLBRR, sNSQLBRR,
			psEnc.sCmn.pulsesLBRR[psEnc.sCmn.nFramesEncoded][:], xfw, xOff)

		// Restore original gains.
		copy(psEncCtrl.gains[:psEnc.sCmn.nbSubfr], tempGains[:psEnc.sCmn.nbSubfr])
	}
}

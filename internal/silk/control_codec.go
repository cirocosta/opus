// Ported from libopus 1.6.1 silk/control_codec.c.

package silk

// controlEncoder is silk_control_encoder: control the encoder.
func (psEnc *encoderStateFLP) controlEncoder(encControl *EncControl, allowBWSwitch, channelNb, forceFsKHz int) int {
	ret := 0

	psEnc.sCmn.useDTX = encControl.UseDTX
	psEnc.sCmn.useCBR = encControl.UseCBR
	psEnc.sCmn.apiFsHz = encControl.APISampleRate
	psEnc.sCmn.maxInternalFsHz = int(encControl.MaxInternalSampleRate)
	psEnc.sCmn.minInternalFsHz = int(encControl.MinInternalSampleRate)
	psEnc.sCmn.desiredInternalFsHz = int(encControl.DesiredInternalSampleRate)
	psEnc.sCmn.useInBandFEC = encControl.UseInBandFEC
	psEnc.sCmn.nChannelsAPI = int(encControl.NChannelsAPI)
	psEnc.sCmn.nChannelsInternal = int(encControl.NChannelsInternal)
	psEnc.sCmn.allowBandwidthSwitch = allowBWSwitch
	psEnc.sCmn.channelNb = channelNb

	if psEnc.sCmn.controlledSinceLastPayload != 0 && psEnc.sCmn.prefillFlag == 0 {
		if psEnc.sCmn.apiFsHz != psEnc.sCmn.prevAPIFsHz && psEnc.sCmn.fsKHz > 0 {
			// Change in API sampling rate in the middle of encoding a
			// packet.
			ret += psEnc.setupResamplers(psEnc.sCmn.fsKHz)
		}
		return ret
	}

	// Beyond this point we know that there are no previously coded
	// frames in the payload buffer.

	// Determine internal sampling rate.
	fsKHz := psEnc.sCmn.controlAudioBandwidth(encControl)
	if forceFsKHz != 0 {
		fsKHz = forceFsKHz
	}
	// Prepare resampler and buffered data.
	ret += psEnc.setupResamplers(fsKHz)

	// Set internal sampling frequency.
	ret += psEnc.setupFs(fsKHz, encControl.PayloadSizeMs)

	// Set encoding complexity.
	ret += psEnc.sCmn.setupComplexity(encControl.Complexity)

	// Set packet loss rate measured by farend.
	psEnc.sCmn.packetLossPerc = encControl.PacketLossPercentage

	// Set LBRR usage.
	ret += psEnc.sCmn.setupLBRR(encControl)

	psEnc.sCmn.controlledSinceLastPayload = 1

	return ret
}

// setupResamplers is silk_setup_resamplers.
func (psEnc *encoderStateFLP) setupResamplers(fsKHz int) int {
	ret := NoError

	if psEnc.sCmn.fsKHz != fsKHz || psEnc.sCmn.prevAPIFsHz != psEnc.sCmn.apiFsHz {
		if psEnc.sCmn.fsKHz == 0 {
			// Initialize the resampler for enc_API.c preparing resampling
			// from API_fs_Hz to fs_kHz.
			ret += resamplerInit(&psEnc.sCmn.resamplerState, psEnc.sCmn.apiFsHz, int32(fsKHz*1000), true)
		} else {
			sc := &psEnc.scratch
			xBufFIX := sc.xBufFIX[:]

			bufLengthMs := lshift(int32(psEnc.sCmn.nbSubfr*5), 1) + laShapeMs
			oldBufSamples := int(bufLengthMs) * psEnc.sCmn.fsKHz

			newBufSamples := int(bufLengthMs) * fsKHz
			float2shortArray(xBufFIX, psEnc.xBuf[:], oldBufSamples)

			// Initialize resampler for temporary resampling of x_buf data
			// to API_fs_Hz.
			tempResamplerState := &sc.tempResampler
			ret += resamplerInit(tempResamplerState, smulbb(int32(psEnc.sCmn.fsKHz), 1000), psEnc.sCmn.apiFsHz, false)

			// Calculate number of samples to temporarily upsample.
			apiBufSamples := int(bufLengthMs * div32_16(psEnc.sCmn.apiFsHz, 1000))

			// Temporary resampling of x_buf data to API_fs_Hz.
			xBufAPIFsHz := sc.xBufAPIFsHz[:]
			ret += resampler(tempResamplerState, xBufAPIFsHz, xBufFIX, oldBufSamples)

			// Initialize the resampler for enc_API.c preparing resampling
			// from API_fs_Hz to fs_kHz.
			ret += resamplerInit(&psEnc.sCmn.resamplerState, psEnc.sCmn.apiFsHz, smulbb(int32(fsKHz), 1000), true)

			// Correct resampler state by resampling buffered data from
			// API_fs_Hz to fs_kHz.
			ret += resampler(&psEnc.sCmn.resamplerState, xBufFIX, xBufAPIFsHz, apiBufSamples)

			short2floatArray(psEnc.xBuf[:], xBufFIX, newBufSamples)
		}
	}

	psEnc.sCmn.prevAPIFsHz = psEnc.sCmn.apiFsHz

	return ret
}

// setupFs is silk_setup_fs.
func (psEnc *encoderStateFLP) setupFs(fsKHz, packetSizeMs int) int {
	ret := NoError

	// Set packet size.
	if packetSizeMs != psEnc.sCmn.packetSizeMs {
		if packetSizeMs != 10 && packetSizeMs != 20 && packetSizeMs != 40 && packetSizeMs != 60 {
			ret = EncPacketSizeNotSupported
		}
		if packetSizeMs <= 10 {
			psEnc.sCmn.nFramesPerPacket = 1
			if packetSizeMs == 10 {
				psEnc.sCmn.nbSubfr = 2
			} else {
				psEnc.sCmn.nbSubfr = 1
			}
			psEnc.sCmn.frameLength = int(smulbb(int32(packetSizeMs), int32(fsKHz)))
			psEnc.sCmn.pitchLPCWinLength = int(smulbb(findPitchLPCWinMs2SF, int32(fsKHz)))
			if psEnc.sCmn.fsKHz == 8 {
				psEnc.sCmn.pitchContourICDF = pitchContour10MsNBICDF[:]
			} else {
				psEnc.sCmn.pitchContourICDF = pitchContour10MsICDF[:]
			}
		} else {
			psEnc.sCmn.nFramesPerPacket = packetSizeMs / maxFrameLengthMs
			psEnc.sCmn.nbSubfr = maxNbSubfr
			psEnc.sCmn.frameLength = int(smulbb(20, int32(fsKHz)))
			psEnc.sCmn.pitchLPCWinLength = int(smulbb(findPitchLPCWinMs, int32(fsKHz)))
			if psEnc.sCmn.fsKHz == 8 {
				psEnc.sCmn.pitchContourICDF = pitchContourNBICDF[:]
			} else {
				psEnc.sCmn.pitchContourICDF = pitchContourICDF[:]
			}
		}
		psEnc.sCmn.packetSizeMs = packetSizeMs
		psEnc.sCmn.targetRateBps = 0 // trigger new SNR computation
	}

	// Set internal sampling frequency.
	if psEnc.sCmn.fsKHz != fsKHz {
		// Reset part of the state.
		psEnc.sShape = shapeStateFLP{}
		psEnc.sCmn.sNSQ = nsqState{}
		psEnc.sCmn.prevNLSFqQ15 = [maxLPCOrder]int16{}
		psEnc.sCmn.sLP.inLPState = [2]int32{}
		psEnc.sCmn.inputBufIx = 0
		psEnc.sCmn.nFramesEncoded = 0
		psEnc.sCmn.targetRateBps = 0 // trigger new SNR computation

		// Initialize non-zero parameters.
		psEnc.sCmn.prevLag = 100
		psEnc.sCmn.firstFrameAfterReset = 1
		psEnc.sShape.lastGainIndex = 10
		psEnc.sCmn.sNSQ.lagPrev = 100
		psEnc.sCmn.sNSQ.prevGainQ16 = 65536
		psEnc.sCmn.prevSignalType = typeNoVoiceActivity

		psEnc.sCmn.fsKHz = fsKHz
		if psEnc.sCmn.fsKHz == 8 {
			if psEnc.sCmn.nbSubfr == maxNbSubfr {
				psEnc.sCmn.pitchContourICDF = pitchContourNBICDF[:]
			} else {
				psEnc.sCmn.pitchContourICDF = pitchContour10MsNBICDF[:]
			}
		} else {
			if psEnc.sCmn.nbSubfr == maxNbSubfr {
				psEnc.sCmn.pitchContourICDF = pitchContourICDF[:]
			} else {
				psEnc.sCmn.pitchContourICDF = pitchContour10MsICDF[:]
			}
		}
		if psEnc.sCmn.fsKHz == 8 || psEnc.sCmn.fsKHz == 12 {
			psEnc.sCmn.predictLPCOrder = minLPCOrder
			psEnc.sCmn.psNLSFCB = &nlsfCBNBMB
		} else {
			psEnc.sCmn.predictLPCOrder = maxLPCOrder
			psEnc.sCmn.psNLSFCB = &nlsfCBWB
		}
		psEnc.sCmn.subfrLength = subFrameLengthMs * fsKHz
		psEnc.sCmn.frameLength = int(smulbb(int32(psEnc.sCmn.subfrLength), int32(psEnc.sCmn.nbSubfr)))
		psEnc.sCmn.ltpMemLength = int(smulbb(ltpMemLengthMs, int32(fsKHz)))
		psEnc.sCmn.laPitch = int(smulbb(laPitchMs, int32(fsKHz)))
		psEnc.sCmn.maxPitchLag = int(smulbb(18, int32(fsKHz)))
		if psEnc.sCmn.nbSubfr == maxNbSubfr {
			psEnc.sCmn.pitchLPCWinLength = int(smulbb(findPitchLPCWinMs, int32(fsKHz)))
		} else {
			psEnc.sCmn.pitchLPCWinLength = int(smulbb(findPitchLPCWinMs2SF, int32(fsKHz)))
		}
		if psEnc.sCmn.fsKHz == 16 {
			psEnc.sCmn.pitchLagLowBitsICDF = uniform8ICDF[:]
		} else if psEnc.sCmn.fsKHz == 12 {
			psEnc.sCmn.pitchLagLowBitsICDF = uniform6ICDF[:]
		} else {
			psEnc.sCmn.pitchLagLowBitsICDF = uniform4ICDF[:]
		}
	}

	return ret
}

// warpingQ16PerKHz is SILK_FIX_CONST( WARPING_MULTIPLIER, 16 ).
var warpingQ16PerKHz = int(fixConstF32(warpingMultiplier, 16))

// setupComplexity is silk_setup_complexity.
func (psEncC *encoderState) setupComplexity(complexity int) int {
	ret := 0

	// Set encoding complexity.
	if complexity < 1 {
		psEncC.pitchEstimationComplexity = silkPEMinComplex
		psEncC.pitchEstimationThresholdQ16 = int32(fixConst(0.8, 16))
		psEncC.pitchEstimationLPCOrder = 6
		psEncC.shapingLPCOrder = 12
		psEncC.laShape = 3 * psEncC.fsKHz
		psEncC.nStatesDelayedDecision = 1
		psEncC.useInterpolatedNLSFs = 0
		psEncC.nlsfMSVQSurvivors = 2
		psEncC.warpingQ16 = 0
	} else if complexity < 2 {
		psEncC.pitchEstimationComplexity = silkPEMidComplex
		psEncC.pitchEstimationThresholdQ16 = int32(fixConst(0.76, 16))
		psEncC.pitchEstimationLPCOrder = 8
		psEncC.shapingLPCOrder = 14
		psEncC.laShape = 5 * psEncC.fsKHz
		psEncC.nStatesDelayedDecision = 1
		psEncC.useInterpolatedNLSFs = 0
		psEncC.nlsfMSVQSurvivors = 3
		psEncC.warpingQ16 = 0
	} else if complexity < 3 {
		psEncC.pitchEstimationComplexity = silkPEMinComplex
		psEncC.pitchEstimationThresholdQ16 = int32(fixConst(0.8, 16))
		psEncC.pitchEstimationLPCOrder = 6
		psEncC.shapingLPCOrder = 12
		psEncC.laShape = 3 * psEncC.fsKHz
		psEncC.nStatesDelayedDecision = 2
		psEncC.useInterpolatedNLSFs = 0
		psEncC.nlsfMSVQSurvivors = 2
		psEncC.warpingQ16 = 0
	} else if complexity < 4 {
		psEncC.pitchEstimationComplexity = silkPEMidComplex
		psEncC.pitchEstimationThresholdQ16 = int32(fixConst(0.76, 16))
		psEncC.pitchEstimationLPCOrder = 8
		psEncC.shapingLPCOrder = 14
		psEncC.laShape = 5 * psEncC.fsKHz
		psEncC.nStatesDelayedDecision = 2
		psEncC.useInterpolatedNLSFs = 0
		psEncC.nlsfMSVQSurvivors = 4
		psEncC.warpingQ16 = 0
	} else if complexity < 6 {
		psEncC.pitchEstimationComplexity = silkPEMidComplex
		psEncC.pitchEstimationThresholdQ16 = int32(fixConst(0.74, 16))
		psEncC.pitchEstimationLPCOrder = 10
		psEncC.shapingLPCOrder = 16
		psEncC.laShape = 5 * psEncC.fsKHz
		psEncC.nStatesDelayedDecision = 2
		psEncC.useInterpolatedNLSFs = 1
		psEncC.nlsfMSVQSurvivors = 6
		psEncC.warpingQ16 = psEncC.fsKHz * warpingQ16PerKHz
	} else if complexity < 8 {
		psEncC.pitchEstimationComplexity = silkPEMidComplex
		psEncC.pitchEstimationThresholdQ16 = int32(fixConst(0.72, 16))
		psEncC.pitchEstimationLPCOrder = 12
		psEncC.shapingLPCOrder = 20
		psEncC.laShape = 5 * psEncC.fsKHz
		psEncC.nStatesDelayedDecision = 3
		psEncC.useInterpolatedNLSFs = 1
		psEncC.nlsfMSVQSurvivors = 8
		psEncC.warpingQ16 = psEncC.fsKHz * warpingQ16PerKHz
	} else {
		psEncC.pitchEstimationComplexity = silkPEMaxComplex
		psEncC.pitchEstimationThresholdQ16 = int32(fixConst(0.7, 16))
		psEncC.pitchEstimationLPCOrder = 16
		psEncC.shapingLPCOrder = 24
		psEncC.laShape = 5 * psEncC.fsKHz
		psEncC.nStatesDelayedDecision = maxDelDecStates
		psEncC.useInterpolatedNLSFs = 1
		psEncC.nlsfMSVQSurvivors = 16
		psEncC.warpingQ16 = psEncC.fsKHz * warpingQ16PerKHz
	}

	// Do not allow higher pitch estimation LPC order than predict LPC
	// order.
	psEncC.pitchEstimationLPCOrder = minInt(psEncC.pitchEstimationLPCOrder, psEncC.predictLPCOrder)
	psEncC.shapeWinLength = subFrameLengthMs*psEncC.fsKHz + 2*psEncC.laShape
	psEncC.complexity = complexity

	return ret
}

// lbrrGainIncreaseCoefQ16 is SILK_FIX_CONST( 0.2, 16 ).
var lbrrGainIncreaseCoefQ16 = fixConst(0.2, 16)

// setupLBRR is silk_setup_LBRR.
func (psEncC *encoderState) setupLBRR(encControl *EncControl) int {
	ret := NoError

	lbrrInPreviousPacket := psEncC.lbrrEnabled
	psEncC.lbrrEnabled = encControl.LBRRCoded
	if psEncC.lbrrEnabled != 0 {
		// Set gain increase for coding LBRR excitation.
		if lbrrInPreviousPacket == 0 {
			// Previous packet did not have LBRR, and was therefore coded
			// at a higher bitrate.
			psEncC.lbrrGainIncreases = 7
		} else {
			psEncC.lbrrGainIncreases = maxInt(7-int(smulwb(int32(psEncC.packetLossPerc), lbrrGainIncreaseCoefQ16)), 3)
		}
	}

	return ret
}

// Ported from libopus 1.6.1 silk/enc_API.c, silk/API.h, silk/control.h
// (encoder control structure) and the silk_encoder super struct of
// silk/float/structs_FLP.h.

package silk

import (
	"github.com/cirocosta/opus/internal/dsp"
	"github.com/cirocosta/opus/internal/entcode"
)

// EncControl is silk_EncControlStruct: the structure for controlling
// encoder operation.
type EncControl struct {
	// I: Number of channels; 1/2.
	NChannelsAPI int32
	// I: Number of channels; 1/2.
	NChannelsInternal int32
	// I: Input signal sampling rate in Hertz;
	// 8000/12000/16000/24000/32000/44100/48000.
	APISampleRate int32
	// I: Maximum internal sampling rate in Hertz; 8000/12000/16000.
	MaxInternalSampleRate int32
	// I: Minimum internal sampling rate in Hertz; 8000/12000/16000.
	MinInternalSampleRate int32
	// I: Soft request for internal sampling rate in Hertz;
	// 8000/12000/16000.
	DesiredInternalSampleRate int32
	// I: Number of samples per packet in milliseconds; 10/20/40/60.
	PayloadSizeMs int
	// I: Bitrate during active speech in bits/second; internally limited.
	BitRate int32
	// I: Uplink packet loss in percent (0-100).
	PacketLossPercentage int
	// I: Complexity mode; 0 is lowest, 10 is highest complexity.
	Complexity int
	// I: Flag to enable in-band Forward Error Correction (FEC); 0/1.
	UseInBandFEC int
	// I: Flag to enable in-band Deep REDundancy (DRED); 0/1.
	UseDRED int
	// I: Flag to actually code in-band Forward Error Correction (FEC) in
	// the current packet; 0/1.
	LBRRCoded int
	// I: Flag to enable discontinuous transmission (DTX); 0/1.
	UseDTX int
	// I: Flag to use constant bitrate.
	UseCBR int
	// I: Maximum number of bits allowed for the frame.
	MaxBits int
	// I: Causes a smooth downmix to mono.
	ToMono int
	// I: Opus encoder is allowing us to switch bandwidth.
	OpusCanSwitch int
	// I: Make frames as independent as possible (but still use LPC).
	ReducedDependency int
	// O: Internal sampling rate used, in Hertz; 8000/12000/16000.
	InternalSampleRate int32
	// O: Flag that bandwidth switching is allowed (because low voice
	// activity).
	AllowBandwidthSwitch int
	// O: Flag that SILK runs in WB mode without variable LP filter (use
	// for switching between WB/SWB/FB).
	InWBmodeWithoutVariableLP int
	// O: Stereo width.
	StereoWidthQ14 int
	// O: Tells the Opus encoder we're ready to switch.
	SwitchReady int
	// O: SILK Signal type.
	SignalType int
	// O: SILK offset (dithering).
	Offset int
}

// Encoder is silk_encoder, the encoder super struct of the float build:
// the stereo state and two channel states.
type Encoder struct {
	sStereo                  stereoEncState
	nBitsUsedLBRR            int32
	nBitsExceeded            int32
	nChannelsAPI             int
	nChannelsInternal        int
	nPrevChannelsInternal    int
	timeSinceSwitchAllowedMs int
	allowBandwidthSwitch     int
	prevDecodeOnlyMiddle     int
	stateFxx                 [encoderNumChannels]encoderStateFLP

	// Scratch buffers for the ALLOC calls in silk_Encode and
	// silk_stereo_LR_to_MS.
	buf           [maxAPIFsKHz * maxFrameLengthMs * maxFramesPerPacket]int16
	stereoScratch stereoScratch
}

// Init is silk_InitEncoder: initialize or reset the encoder. channels is
// the number of channels the encoder was created for (1 or 2); the C
// code leaves the second channel state untouched for mono, this port
// clears it too. encStatus receives the encoder status.
func (e *Encoder) Init(channels int, encStatus *EncControl) int {
	ret := NoError

	// Reset encoder.
	*e = Encoder{}
	for n := 0; n < channels; n++ {
		ret += e.stateFxx[n].initEncoder()
	}

	e.nChannelsAPI = 1
	e.nChannelsInternal = 1

	// Read control structure.
	ret += e.queryEncoder(encStatus)

	return ret
}

// queryEncoder is silk_QueryEncoder: read the control structure from the
// encoder.
func (e *Encoder) queryEncoder(encStatus *EncControl) int {
	ret := NoError
	stateFxx := &e.stateFxx[0].sCmn

	encStatus.NChannelsAPI = int32(e.nChannelsAPI)
	encStatus.NChannelsInternal = int32(e.nChannelsInternal)
	encStatus.APISampleRate = stateFxx.apiFsHz
	encStatus.MaxInternalSampleRate = int32(stateFxx.maxInternalFsHz)
	encStatus.MinInternalSampleRate = int32(stateFxx.minInternalFsHz)
	encStatus.DesiredInternalSampleRate = int32(stateFxx.desiredInternalFsHz)
	encStatus.PayloadSizeMs = stateFxx.packetSizeMs
	encStatus.BitRate = stateFxx.targetRateBps
	encStatus.PacketLossPercentage = stateFxx.packetLossPerc
	encStatus.Complexity = stateFxx.complexity
	encStatus.UseInBandFEC = stateFxx.useInBandFEC
	encStatus.UseDTX = stateFxx.useDTX
	encStatus.UseCBR = stateFxx.useCBR
	encStatus.InternalSampleRate = smulbb(int32(stateFxx.fsKHz), 1000)
	encStatus.AllowBandwidthSwitch = stateFxx.allowBandwidthSwitch
	encStatus.InWBmodeWithoutVariableLP = 0
	if stateFxx.fsKHz == 16 && stateFxx.sLP.mode == 0 {
		encStatus.InWBmodeWithoutVariableLP = 1
	}

	return ret
}

// speechActThrForSwitchSlopeQ24 is SILK_FIX_CONST( ( 1 -
// SPEECH_ACTIVITY_DTX_THRES ) / MAX_BANDWIDTH_SWITCH_DELAY_MS, 16 + 8 ).
var speechActThrForSwitchSlopeQ24 = fixConstF32((1-speechActivityDTXThres)/maxBandwidthSwitchDelayMs, 16+8)

// Encode is silk_Encode: encode frames with SILK. samplesIn holds
// nSamplesIn samples per API channel, interleaved, as opus_res (float,
// scaled by 1/32768), which is what the float build takes. nBytesOut is
// the number of bytes in the payload (input: max bytes). If prefillFlag
// is set, the input must contain 10 ms of audio, irrespective of what
// encControl.PayloadSizeMs is set to. activity is the decision of the
// Opus voice activity detector. The arch argument of the C function is
// not ported.
func (e *Encoder) Encode(encControl *EncControl, samplesIn []float32, nSamplesIn int, psRangeEnc *entcode.Encoder,
	nBytesOut *int32, prefillFlag int, activity int) int {
	var (
		tmpPayloadSizeMs, tmpComplexity int
		ret                             int
		msTargetRatesBps                [2]int32
	)

	if encControl.ReducedDependency != 0 {
		for n := 0; n < int(encControl.NChannelsAPI); n++ {
			e.stateFxx[n].sCmn.firstFrameAfterReset = 1
		}
	}
	for n := 0; n < int(encControl.NChannelsAPI); n++ {
		e.stateFxx[n].sCmn.nFramesEncoded = 0
	}
	// Check values in encoder control structure.
	if ret = checkControlInput(encControl); ret != 0 {
		return ret
	}

	encControl.SwitchReady = 0

	if int(encControl.NChannelsInternal) > e.nChannelsInternal {
		// Mono -> Stereo transition: init state of second channel and
		// stereo state.
		ret += e.stateFxx[1].initEncoder()
		e.sStereo.predPrevQ13 = [2]int16{}
		e.sStereo.sSide = [2]int16{}
		e.sStereo.midSideAmpQ0[0] = 0
		e.sStereo.midSideAmpQ0[1] = 1
		e.sStereo.midSideAmpQ0[2] = 0
		e.sStereo.midSideAmpQ0[3] = 1
		e.sStereo.widthPrevQ14 = 0
		e.sStereo.smthWidthQ14 = 1 << 14
		if e.nChannelsAPI == 2 {
			e.stateFxx[1].sCmn.resamplerState = e.stateFxx[0].sCmn.resamplerState
			e.stateFxx[1].sCmn.inHPState = e.stateFxx[0].sCmn.inHPState
		}
	}

	transition := encControl.PayloadSizeMs != e.stateFxx[0].sCmn.packetSizeMs ||
		e.nChannelsInternal != int(encControl.NChannelsInternal)

	e.nChannelsAPI = int(encControl.NChannelsAPI)
	e.nChannelsInternal = int(encControl.NChannelsInternal)

	nBlocksOf10ms := int(div32(int32(100*nSamplesIn), encControl.APISampleRate))
	totBlocks := 1
	if nBlocksOf10ms > 1 {
		totBlocks = nBlocksOf10ms >> 1
	}
	currBlock := 0
	if prefillFlag != 0 {
		var saveLP lpState
		// Only accept input length of 10 ms.
		if nBlocksOf10ms != 1 {
			return EncInputInvalidNoOfSamples
		}
		if prefillFlag == 2 {
			saveLP = e.stateFxx[0].sCmn.sLP
			// Save the sampling rate so the bandwidth switching code can
			// keep handling transitions.
			saveLP.savedFsKHz = int32(e.stateFxx[0].sCmn.fsKHz)
		}
		// Reset Encoder.
		for n := 0; n < int(encControl.NChannelsInternal); n++ {
			ret = e.stateFxx[n].initEncoder()
			// Restore the variable LP state.
			if prefillFlag == 2 {
				e.stateFxx[n].sCmn.sLP = saveLP
			}
		}
		tmpPayloadSizeMs = encControl.PayloadSizeMs
		encControl.PayloadSizeMs = 10
		tmpComplexity = encControl.Complexity
		encControl.Complexity = 0
		for n := 0; n < int(encControl.NChannelsInternal); n++ {
			e.stateFxx[n].sCmn.controlledSinceLastPayload = 0
			e.stateFxx[n].sCmn.prefillFlag = 1
		}
	} else {
		// Only accept input lengths that are a multiple of 10 ms.
		if int32(nBlocksOf10ms)*encControl.APISampleRate != int32(100*nSamplesIn) || nSamplesIn < 0 {
			return EncInputInvalidNoOfSamples
		}
		// Make sure no more than one packet can be produced.
		if 1000*int32(nSamplesIn) > int32(encControl.PayloadSizeMs)*encControl.APISampleRate {
			return EncInputInvalidNoOfSamples
		}
	}

	for n := 0; n < int(encControl.NChannelsInternal); n++ {
		// Force the side channel to the same rate as the mid.
		forceFsKHz := 0
		if n == 1 {
			forceFsKHz = e.stateFxx[0].sCmn.fsKHz
		}
		if ret = e.stateFxx[n].controlEncoder(encControl, e.allowBandwidthSwitch, n, forceFsKHz); ret != 0 {
			return ret
		}
		if e.stateFxx[n].sCmn.firstFrameAfterReset != 0 || transition {
			for i := 0; i < e.stateFxx[0].sCmn.nFramesPerPacket; i++ {
				e.stateFxx[n].sCmn.lbrrFlags[i] = 0
			}
		}
		e.stateFxx[n].sCmn.inDTX = e.stateFxx[n].sCmn.useDTX
	}

	// Input buffering/resampling and encoding.
	nSamplesToBufferMax := 10 * nBlocksOf10ms * e.stateFxx[0].sCmn.fsKHz
	buf := e.buf[:]
	in := samplesIn
	for {
		currNBitsUsedLBRR := 0
		nSamplesToBuffer := e.stateFxx[0].sCmn.frameLength - e.stateFxx[0].sCmn.inputBufIx
		nSamplesToBuffer = minInt(nSamplesToBuffer, nSamplesToBufferMax)
		nSamplesFromInput := int(div32_16(int32(nSamplesToBuffer)*e.stateFxx[0].sCmn.apiFsHz, int32(e.stateFxx[0].sCmn.fsKHz*1000)))
		// Resample and write to buffer.
		if encControl.NChannelsAPI == 2 && encControl.NChannelsInternal == 2 {
			id := e.stateFxx[0].sCmn.nFramesEncoded
			for n := 0; n < nSamplesFromInput; n++ {
				buf[n] = dsp.Res2Int16(in[2*n])
			}
			// Making sure to start both resamplers from the same state when
			// switching from mono to stereo.
			if e.nPrevChannelsInternal == 1 && id == 0 {
				e.stateFxx[1].sCmn.resamplerState = e.stateFxx[0].sCmn.resamplerState
			}

			ret += resampler(&e.stateFxx[0].sCmn.resamplerState,
				e.stateFxx[0].sCmn.inputBuf[e.stateFxx[0].sCmn.inputBufIx+2:], buf, nSamplesFromInput)
			e.stateFxx[0].sCmn.inputBufIx += nSamplesToBuffer

			nSamplesToBuffer = e.stateFxx[1].sCmn.frameLength - e.stateFxx[1].sCmn.inputBufIx
			nSamplesToBuffer = minInt(nSamplesToBuffer, 10*nBlocksOf10ms*e.stateFxx[1].sCmn.fsKHz)
			for n := 0; n < nSamplesFromInput; n++ {
				buf[n] = dsp.Res2Int16(in[2*n+1])
			}
			ret += resampler(&e.stateFxx[1].sCmn.resamplerState,
				e.stateFxx[1].sCmn.inputBuf[e.stateFxx[1].sCmn.inputBufIx+2:], buf, nSamplesFromInput)

			e.stateFxx[1].sCmn.inputBufIx += nSamplesToBuffer
		} else if encControl.NChannelsAPI == 2 && encControl.NChannelsInternal == 1 {
			// Combine left and right channels before resampling.
			for n := 0; n < nSamplesFromInput; n++ {
				sum := int32(dsp.Res2Int16(in[2*n] + in[2*n+1]))
				buf[n] = int16(rshiftRound(sum, 1))
			}
			ret += resampler(&e.stateFxx[0].sCmn.resamplerState,
				e.stateFxx[0].sCmn.inputBuf[e.stateFxx[0].sCmn.inputBufIx+2:], buf, nSamplesFromInput)
			// On the first mono frame, average the results for the two
			// resampler states.
			if e.nPrevChannelsInternal == 2 && e.stateFxx[0].sCmn.nFramesEncoded == 0 {
				ret += resampler(&e.stateFxx[1].sCmn.resamplerState,
					e.stateFxx[1].sCmn.inputBuf[e.stateFxx[1].sCmn.inputBufIx+2:], buf, nSamplesFromInput)
				for n := 0; n < e.stateFxx[0].sCmn.frameLength; n++ {
					i0 := e.stateFxx[0].sCmn.inputBufIx + n + 2
					i1 := e.stateFxx[1].sCmn.inputBufIx + n + 2
					e.stateFxx[0].sCmn.inputBuf[i0] = int16((int32(e.stateFxx[0].sCmn.inputBuf[i0]) +
						int32(e.stateFxx[1].sCmn.inputBuf[i1])) >> 1)
				}
			}
			e.stateFxx[0].sCmn.inputBufIx += nSamplesToBuffer
		} else {
			for n := 0; n < nSamplesFromInput; n++ {
				buf[n] = dsp.Res2Int16(in[n])
			}
			ret += resampler(&e.stateFxx[0].sCmn.resamplerState,
				e.stateFxx[0].sCmn.inputBuf[e.stateFxx[0].sCmn.inputBufIx+2:], buf, nSamplesFromInput)
			e.stateFxx[0].sCmn.inputBufIx += nSamplesToBuffer
		}

		in = in[nSamplesFromInput*int(encControl.NChannelsAPI):]
		nSamplesIn -= nSamplesFromInput

		// Default.
		e.allowBandwidthSwitch = 0

		// Silk encoder.
		if e.stateFxx[0].sCmn.inputBufIx >= e.stateFxx[0].sCmn.frameLength {
			// Enough data in input buffer, so encode.

			// Deal with LBRR data.
			if e.stateFxx[0].sCmn.nFramesEncoded == 0 && prefillFlag == 0 {
				// Create space at start of payload for VAD and FEC flags.
				var iCDF [2]uint8
				iCDF[0] = uint8(256 - (int(256) >> uint((e.stateFxx[0].sCmn.nFramesPerPacket+1)*int(encControl.NChannelsInternal))))
				psRangeEnc.EncIcdf(0, iCDF[:], 8)
				currNBitsUsedLBRR = psRangeEnc.Tell()

				// Encode any LBRR data from previous packet.
				// Encode LBRR flags.
				for n := 0; n < int(encControl.NChannelsInternal); n++ {
					lbrrSymbol := 0
					for i := 0; i < e.stateFxx[n].sCmn.nFramesPerPacket; i++ {
						lbrrSymbol |= e.stateFxx[n].sCmn.lbrrFlags[i] << uint(i)
					}
					if lbrrSymbol > 0 {
						e.stateFxx[n].sCmn.lbrrFlag = 1
					} else {
						e.stateFxx[n].sCmn.lbrrFlag = 0
					}
					if lbrrSymbol != 0 && e.stateFxx[n].sCmn.nFramesPerPacket > 1 {
						psRangeEnc.EncIcdf(lbrrSymbol-1, lbrrFlagsICDFPtr[e.stateFxx[n].sCmn.nFramesPerPacket-2], 8)
					}
				}

				// Code LBRR indices and excitation signals.
				for i := 0; i < e.stateFxx[0].sCmn.nFramesPerPacket; i++ {
					for n := 0; n < int(encControl.NChannelsInternal); n++ {
						if e.stateFxx[n].sCmn.lbrrFlags[i] != 0 {
							if encControl.NChannelsInternal == 2 && n == 0 {
								stereoEncodePred(psRangeEnc, &e.sStereo.predIx[i])
								// For LBRR data there's no need to code the
								// mid-only flag if the side-channel LBRR flag
								// is set.
								if e.stateFxx[1].sCmn.lbrrFlags[i] == 0 {
									stereoEncodeMidOnly(psRangeEnc, e.sStereo.midOnlyFlags[i])
								}
							}
							// Use conditional coding if previous frame
							// available.
							condCoding := codeIndependently
							if i > 0 && e.stateFxx[n].sCmn.lbrrFlags[i-1] != 0 {
								condCoding = codeConditionally
							}
							e.stateFxx[n].sCmn.encodeIndices(psRangeEnc, i, true, condCoding)
							encodePulses(psRangeEnc, int(e.stateFxx[n].sCmn.indicesLBRR[i].signalType),
								int(e.stateFxx[n].sCmn.indicesLBRR[i].quantOffsetType),
								e.stateFxx[n].sCmn.pulsesLBRR[i][:], e.stateFxx[n].sCmn.frameLength)
						}
					}
				}

				// Reset LBRR flags.
				for n := 0; n < int(encControl.NChannelsInternal); n++ {
					e.stateFxx[n].sCmn.lbrrFlags = [maxFramesPerPacket]int{}
				}
				currNBitsUsedLBRR = psRangeEnc.Tell() - currNBitsUsedLBRR
			}

			hpVariableCutoff(&e.stateFxx)

			// Total target bits for packet.
			nBits := div32_16(mul(encControl.BitRate, int32(encControl.PayloadSizeMs)), 1000)
			// Subtract bits used for LBRR.
			if prefillFlag == 0 {
				// psEnc->nBitsUsedLBRR is an exponential moving average of the
				// LBRR usage, except that for the first LBRR frame it does no
				// averaging and for the first frame after after LBRR, it goes
				// back to zero immediately.
				if currNBitsUsedLBRR < 10 {
					e.nBitsUsedLBRR = 0
				} else if e.nBitsUsedLBRR < 10 {
					e.nBitsUsedLBRR = int32(currNBitsUsedLBRR)
				} else {
					e.nBitsUsedLBRR = (e.nBitsUsedLBRR + int32(currNBitsUsedLBRR)) / 2
				}
				nBits -= e.nBitsUsedLBRR
			}
			// Divide by number of uncoded frames left in packet.
			nBits = div32_16(nBits, int32(e.stateFxx[0].sCmn.nFramesPerPacket))
			// Convert to bits/second.
			var targetRateBps int32
			if encControl.PayloadSizeMs == 10 {
				targetRateBps = smulbb(nBits, 100)
			} else {
				targetRateBps = smulbb(nBits, 50)
			}
			// Subtract fraction of bits in excess of target in previous
			// frames and packets.
			targetRateBps -= div32_16(mul(e.nBitsExceeded, 1000), bitreservoirDecayTimeMs)
			if prefillFlag == 0 && e.stateFxx[0].sCmn.nFramesEncoded > 0 {
				// Compare actual vs target bits so far in this packet.
				bitsBalance := int32(psRangeEnc.Tell()) - e.nBitsUsedLBRR - nBits*int32(e.stateFxx[0].sCmn.nFramesEncoded)
				targetRateBps -= div32_16(mul(bitsBalance, 1000), bitreservoirDecayTimeMs)
			}
			// Never exceed input bitrate.
			targetRateBps = limit32(targetRateBps, encControl.BitRate, 5000)

			// Convert Left/Right to Mid/Side.
			if encControl.NChannelsInternal == 2 {
				fe := e.stateFxx[0].sCmn.nFramesEncoded
				stereoLRToMS(&e.sStereo, e.stateFxx[0].sCmn.inputBuf[:], e.stateFxx[1].sCmn.inputBuf[:],
					&e.sStereo.predIx[fe], &e.sStereo.midOnlyFlags[fe],
					&msTargetRatesBps, targetRateBps, e.stateFxx[0].sCmn.speechActivityQ8, encControl.ToMono,
					e.stateFxx[0].sCmn.fsKHz, e.stateFxx[0].sCmn.frameLength, &e.stereoScratch)
				if e.sStereo.midOnlyFlags[fe] == 0 {
					// Reset side channel encoder memory for first frame with
					// side coding.
					if e.prevDecodeOnlyMiddle == 1 {
						s1 := &e.stateFxx[1]
						s1.sShape = shapeStateFLP{}
						s1.sCmn.sNSQ = nsqState{}
						s1.sCmn.prevNLSFqQ15 = [maxLPCOrder]int16{}
						s1.sCmn.sLP.inLPState = [2]int32{}
						s1.sCmn.prevLag = 100
						s1.sCmn.sNSQ.lagPrev = 100
						s1.sShape.lastGainIndex = 10
						s1.sCmn.prevSignalType = typeNoVoiceActivity
						s1.sCmn.sNSQ.prevGainQ16 = 65536
						s1.sCmn.firstFrameAfterReset = 1
					}
					e.stateFxx[1].encodeDoVADFLP(activity)
				} else {
					e.stateFxx[1].sCmn.vadFlags[fe] = 0
				}
				if prefillFlag == 0 {
					stereoEncodePred(psRangeEnc, &e.sStereo.predIx[fe])
					if e.stateFxx[1].sCmn.vadFlags[fe] == 0 {
						stereoEncodeMidOnly(psRangeEnc, e.sStereo.midOnlyFlags[fe])
					}
				}
			} else {
				// Buffering.
				copy(e.stateFxx[0].sCmn.inputBuf[:2], e.sStereo.sMid[:])
				fl := e.stateFxx[0].sCmn.frameLength
				copy(e.sStereo.sMid[:], e.stateFxx[0].sCmn.inputBuf[fl:fl+2])
			}
			e.stateFxx[0].encodeDoVADFLP(activity)

			// Encode.
			for n := 0; n < int(encControl.NChannelsInternal); n++ {
				// Handling rate constraints.
				maxBits := encControl.MaxBits
				if totBlocks == 2 && currBlock == 0 {
					maxBits = maxBits * 3 / 5
				} else if totBlocks == 3 {
					if currBlock == 0 {
						maxBits = maxBits * 2 / 5
					} else if currBlock == 1 {
						maxBits = maxBits * 3 / 4
					}
				}
				useCBR := 0
				if encControl.UseCBR != 0 && currBlock == totBlocks-1 {
					useCBR = 1
				}

				var channelRateBps int32
				if encControl.NChannelsInternal == 1 {
					channelRateBps = targetRateBps
				} else {
					channelRateBps = msTargetRatesBps[n]
					if n == 0 && msTargetRatesBps[1] > 0 {
						useCBR = 0
						// Give mid up to 1/2 of the max bits for that frame.
						maxBits -= encControl.MaxBits / (totBlocks * 2)
					}
				}

				if channelRateBps > 0 {
					e.stateFxx[n].sCmn.controlSNR(channelRateBps)

					// Use independent coding if no previous frame available.
					var condCoding int
					if e.stateFxx[0].sCmn.nFramesEncoded-n <= 0 {
						condCoding = codeIndependently
					} else if n > 0 && e.prevDecodeOnlyMiddle != 0 {
						// If we skipped a side frame in this packet, we don't
						// need LTP scaling; the LTP state is well-defined.
						condCoding = codeIndependentlyNoLTPScaling
					} else {
						condCoding = codeConditionally
					}
					ret = e.stateFxx[n].encodeFrameFLP(nBytesOut, psRangeEnc, condCoding, maxBits, useCBR)
				}
				e.stateFxx[n].sCmn.controlledSinceLastPayload = 0
				e.stateFxx[n].sCmn.inputBufIx = 0
				e.stateFxx[n].sCmn.nFramesEncoded++
			}
			e.prevDecodeOnlyMiddle = int(e.sStereo.midOnlyFlags[e.stateFxx[0].sCmn.nFramesEncoded-1])

			// Insert VAD and FEC flags at beginning of bitstream.
			if *nBytesOut > 0 && e.stateFxx[0].sCmn.nFramesEncoded == e.stateFxx[0].sCmn.nFramesPerPacket {
				flags := 0
				for n := 0; n < int(encControl.NChannelsInternal); n++ {
					for i := 0; i < e.stateFxx[n].sCmn.nFramesPerPacket; i++ {
						flags = flags << 1
						flags |= int(e.stateFxx[n].sCmn.vadFlags[i])
					}
					flags = flags << 1
					flags |= int(e.stateFxx[n].sCmn.lbrrFlag)
				}
				if prefillFlag == 0 {
					psRangeEnc.PatchInitialBits(uint32(flags), uint32((e.stateFxx[0].sCmn.nFramesPerPacket+1)*int(encControl.NChannelsInternal)))
				}

				// Return zero bytes if all channels DTXed.
				if e.stateFxx[0].sCmn.inDTX != 0 && (encControl.NChannelsInternal == 1 || e.stateFxx[1].sCmn.inDTX != 0) {
					*nBytesOut = 0
				}

				e.nBitsExceeded += *nBytesOut * 8
				e.nBitsExceeded -= div32_16(mul(encControl.BitRate, int32(encControl.PayloadSizeMs)), 1000)
				e.nBitsExceeded = limit32(e.nBitsExceeded, 0, 10000)

				// Update flag indicating if bandwidth switching is allowed.
				speechActThrForSwitchQ8 := smlawb(fixConstF32(speechActivityDTXThres, 8),
					speechActThrForSwitchSlopeQ24, int32(e.timeSinceSwitchAllowedMs))
				if int32(e.stateFxx[0].sCmn.speechActivityQ8) < speechActThrForSwitchQ8 {
					e.allowBandwidthSwitch = 1
					e.timeSinceSwitchAllowedMs = 0
				} else {
					e.allowBandwidthSwitch = 0
					e.timeSinceSwitchAllowedMs += encControl.PayloadSizeMs
				}
			}

			if nSamplesIn == 0 {
				break
			}
		} else {
			break
		}
		currBlock++
	}

	e.nPrevChannelsInternal = int(encControl.NChannelsInternal)

	encControl.AllowBandwidthSwitch = e.allowBandwidthSwitch
	encControl.InWBmodeWithoutVariableLP = 0
	if e.stateFxx[0].sCmn.fsKHz == 16 && e.stateFxx[0].sCmn.sLP.mode == 0 {
		encControl.InWBmodeWithoutVariableLP = 1
	}
	encControl.InternalSampleRate = smulbb(int32(e.stateFxx[0].sCmn.fsKHz), 1000)
	if encControl.ToMono != 0 {
		encControl.StereoWidthQ14 = 0
	} else {
		encControl.StereoWidthQ14 = int(e.sStereo.smthWidthQ14)
	}
	if prefillFlag != 0 {
		encControl.PayloadSizeMs = tmpPayloadSizeMs
		encControl.Complexity = tmpComplexity
		for n := 0; n < int(encControl.NChannelsInternal); n++ {
			e.stateFxx[n].sCmn.controlledSinceLastPayload = 0
			e.stateFxx[n].sCmn.prefillFlag = 0
		}
	}

	encControl.SignalType = int(e.stateFxx[0].sCmn.indices.signalType)
	encControl.Offset = int(quantizationOffsetsQ10[e.stateFxx[0].sCmn.indices.signalType>>1][e.stateFxx[0].sCmn.indices.quantOffsetType])
	return ret
}

// Ported from libopus 1.6.1 silk/dec_API.c, silk/API.h and silk/control.h
// (decoder control structure).

package silk

import "github.com/cirocosta/opus/internal/entcode"

// DecControl is silk_DecControlStruct: the structure for controlling
// decoder operation and reading decoder status.
type DecControl struct {
	// I: Number of channels; 1/2.
	NChannelsAPI int32
	// I: Number of channels; 1/2.
	NChannelsInternal int32
	// I: Output signal sampling rate in Hertz;
	// 8000/12000/16000/24000/32000/44100/48000.
	APISampleRate int32
	// I: Internal sampling rate used, in Hertz; 8000/12000/16000.
	InternalSampleRate int32
	// I: Number of samples per packet in milliseconds; 10/20/40/60.
	PayloadSizeMs int
	// O: Pitch lag of previous frame (0 if unvoiced), measured in samples
	// at 48 kHz.
	PrevPitchLag int
}

// Decoder is silk_decoder, the decoder super struct: two channel
// states and the stereo state.
type Decoder struct {
	channelState         [decoderNumChannels]decoderState
	sStereo              stereoDecState
	nChannelsAPI         int
	nChannelsInternal    int
	prevDecodeOnlyMiddle int

	// Scratch buffers for the ALLOC calls in silk_Decode.
	samplesOut1Tmp [decoderNumChannels][maxFrameLength + 2]int16
	samplesOut2Tmp [maxAPIFsKHz * maxFrameLengthMs]int16
	out16          [decoderNumChannels * maxAPIFsKHz * maxFrameLengthMs]int16
}

// Reset is silk_ResetDecoder: reset decoder state.
func (d *Decoder) Reset() int {
	ret := NoError
	for n := 0; n < decoderNumChannels; n++ {
		ret = d.channelState[n].resetDecoder()
	}
	d.sStereo = stereoDecState{}
	// Not strictly needed, but it's cleaner that way.
	d.prevDecodeOnlyMiddle = 0

	return ret
}

// Init is silk_InitDecoder: initialize decoder state.
func (d *Decoder) Init() int {
	ret := NoError
	for n := 0; n < decoderNumChannels; n++ {
		ret = d.channelState[n].initDecoder()
	}
	d.sStereo = stereoDecState{}
	d.nChannelsAPI = 0
	d.nChannelsInternal = 0
	// Not strictly needed, but it's cleaner that way.
	d.prevDecodeOnlyMiddle = 0

	return ret
}

// Decode is silk_Decode: decode a frame. samplesOut receives
// nSamplesOut samples per API channel, interleaved, as opus_res (float,
// scaled by 1/32768), which is what the float build outputs. lostFlag is
// FlagDecodeNormal, FlagPacketLost or FlagDecodeLBRR. newPacketFlag
// marks the first decoder call for this packet. psRangeDec is not read
// when lostFlag is FlagPacketLost and may then be nil. The arch
// argument of the C function is not ported.
func (d *Decoder) Decode(decControl *DecControl, lostFlag int, newPacketFlag bool,
	psRangeDec *entcode.Decoder, samplesOut []float32,
) (nSamplesOut int32, ret int) {
	nSamplesOut, ret = d.decode(decControl, lostFlag, newPacketFlag, psRangeDec, d.out16[:])
	if ret != NoError && nSamplesOut == 0 {
		return nSamplesOut, ret
	}
	n := int(nSamplesOut) * int(decControl.NChannelsAPI)
	for i := 0; i < n; i++ {
		samplesOut[i] = int16ToRes(d.out16[i])
	}
	return nSamplesOut, ret
}

// DecodeInt16 is Decode with opus_int16 output (the samples before the
// INT16TORES conversion).
func (d *Decoder) DecodeInt16(decControl *DecControl, lostFlag int, newPacketFlag bool,
	psRangeDec *entcode.Decoder, samplesOut []int16,
) (nSamplesOut int32, ret int) {
	return d.decode(decControl, lostFlag, newPacketFlag, psRangeDec, samplesOut)
}

// int16ToRes is INT16TORES in the float build: a * (1/CELT_SIG_SCALE).
func int16ToRes(a int16) float32 {
	return float32(a) * (1.0 / 32768.0)
}

// decode is the body of silk_Decode with int16 output.
// preserve the reference prediction, gain and numerical decision sequence
//
//revive:disable-next-line:cognitive-complexity
func (d *Decoder) decode(decControl *DecControl, lostFlag int, newPacketFlag bool,
	psRangeDec *entcode.Decoder, samplesOut []int16,
) (int32, int) {
	var nSamplesOutDec int
	var msPredQ13 [2]int32
	decodeOnlyMiddle := 0
	ret := NoError
	channelState := &d.channelState
	nChInternal := int(decControl.NChannelsInternal)
	nChAPI := int(decControl.NChannelsAPI)

	// Test if first frame in payload.
	if newPacketFlag {
		for n := 0; n < nChInternal; n++ {
			channelState[n].nFramesDecoded = 0 // Used to count frames in packet
		}
	}

	// If Mono -> Stereo transition in bitstream: init state of second
	// channel.
	if nChInternal > d.nChannelsInternal {
		ret += channelState[1].initDecoder()
	}

	stereoToMono := nChInternal == 1 && d.nChannelsInternal == 2 &&
		(decControl.InternalSampleRate == int32(1000*channelState[0].fsKHz))

	if channelState[0].nFramesDecoded == 0 {
		for n := 0; n < nChInternal; n++ {
			switch decControl.PayloadSizeMs {
			case 0:
				// Assuming packet loss, use 10 ms.
				channelState[n].nFramesPerPacket = 1
				channelState[n].nbSubfr = 2
			case 10:
				channelState[n].nFramesPerPacket = 1
				channelState[n].nbSubfr = 2
			case 20:
				channelState[n].nFramesPerPacket = 1
				channelState[n].nbSubfr = 4
			case 40:
				channelState[n].nFramesPerPacket = 2
				channelState[n].nbSubfr = 4
			case 60:
				channelState[n].nFramesPerPacket = 3
				channelState[n].nbSubfr = 4
			default:
				return 0, DecInvalidFrameSize
			}
			fsKHzDec := int(decControl.InternalSampleRate>>10) + 1
			if fsKHzDec != 8 && fsKHzDec != 12 && fsKHzDec != 16 {
				return 0, DecInvalidSamplingFrequency
			}
			ret += channelState[n].decoderSetFs(fsKHzDec, decControl.APISampleRate)
		}
	}

	if nChAPI == 2 && nChInternal == 2 && (d.nChannelsAPI == 1 || d.nChannelsInternal == 1) {
		d.sStereo.predPrevQ13 = [2]int16{}
		d.sStereo.sSide = [2]int16{}
		channelState[1].resamplerState = channelState[0].resamplerState
	}
	d.nChannelsAPI = nChAPI
	d.nChannelsInternal = nChInternal

	if decControl.APISampleRate > int32(maxAPIFsKHz)*1000 || decControl.APISampleRate < 8000 {
		ret = DecInvalidSamplingFrequency
		return 0, ret
	}

	if lostFlag != FlagPacketLost && channelState[0].nFramesDecoded == 0 {
		// First decoder call for this payload.
		// Decode VAD flags and LBRR flag.
		for n := 0; n < nChInternal; n++ {
			for i := 0; i < channelState[n].nFramesPerPacket; i++ {
				channelState[n].vadFlags[i] = psRangeDec.DecBitLogp(1)
			}
			channelState[n].lbrrFlag = psRangeDec.DecBitLogp(1)
		}
		// Decode LBRR flags.
		for n := 0; n < nChInternal; n++ {
			channelState[n].lbrrFlags = [maxFramesPerPacket]int{}
			if channelState[n].lbrrFlag != 0 {
				if channelState[n].nFramesPerPacket == 1 {
					channelState[n].lbrrFlags[0] = 1
				} else {
					lbrrSymbol := psRangeDec.DecIcdf(
						lbrrFlagsICDFPtr[channelState[n].nFramesPerPacket-2],
						8,
					) + 1
					for i := 0; i < channelState[n].nFramesPerPacket; i++ {
						channelState[n].lbrrFlags[i] = (lbrrSymbol >> uint(i)) & 1
					}
				}
			}
		}

		if lostFlag == FlagDecodeNormal {
			// Regular decoding: skip all LBRR data.
			for i := 0; i < channelState[0].nFramesPerPacket; i++ {
				for n := 0; n < nChInternal; n++ {
					if channelState[n].lbrrFlags[i] != 0 {
						var condCoding int

						if nChInternal == 2 && n == 0 {
							stereoDecodePred(psRangeDec, &msPredQ13)
							if channelState[1].lbrrFlags[i] == 0 {
								decodeOnlyMiddle = stereoDecodeMidOnly(psRangeDec)
							}
						}
						// Use conditional coding if previous frame available.
						if i > 0 && channelState[n].lbrrFlags[i-1] != 0 {
							condCoding = codeConditionally
						} else {
							condCoding = codeIndependently
						}
						channelState[n].decodeIndices(psRangeDec, i, true, condCoding)
						pulses := channelState[n].scratch.pulses[:]
						decodePulses(
							psRangeDec,
							pulses,
							int(channelState[n].indices.signalType),
							int(
								channelState[n].indices.quantOffsetType,
							),
							channelState[n].frameLength,
						)
					}
				}
			}
		}
	}

	// Get MS predictor index.
	if nChInternal == 2 {
		if lostFlag == FlagDecodeNormal ||
			(lostFlag == FlagDecodeLBRR && channelState[0].lbrrFlags[channelState[0].nFramesDecoded] == 1) {
			stereoDecodePred(psRangeDec, &msPredQ13)
			// For LBRR data, decode mid-only flag only if side-channel's
			// LBRR flag is false.
			if (lostFlag == FlagDecodeNormal && channelState[1].vadFlags[channelState[0].nFramesDecoded] == 0) ||
				(lostFlag == FlagDecodeLBRR && channelState[1].lbrrFlags[channelState[0].nFramesDecoded] == 0) {
				decodeOnlyMiddle = stereoDecodeMidOnly(psRangeDec)
			} else {
				decodeOnlyMiddle = 0
			}
		} else {
			for n := 0; n < 2; n++ {
				msPredQ13[n] = int32(d.sStereo.predPrevQ13[n])
			}
		}
	}

	// Reset side channel decoder prediction memory for first frame with
	// side coding.
	if nChInternal == 2 && decodeOnlyMiddle == 0 && d.prevDecodeOnlyMiddle == 1 {
		cs1 := &d.channelState[1]
		cs1.outBuf = [maxFrameLength + 2*maxSubFrameLength]int16{}
		cs1.sLPCQ14Buf = [maxLPCOrder]int32{}
		cs1.lagPrev = 100
		cs1.lastGainIndex = 10
		cs1.prevSignalType = typeNoVoiceActivity
		cs1.firstFrameAfterReset = 1
	}

	frameLength := channelState[0].frameLength
	samplesOut1Tmp := [2][]int16{
		d.samplesOut1Tmp[0][:frameLength+2],
		d.samplesOut1Tmp[1][:frameLength+2],
	}

	var hasSide bool
	if lostFlag == FlagDecodeNormal {
		hasSide = decodeOnlyMiddle == 0
	} else {
		hasSide = d.prevDecodeOnlyMiddle == 0 ||
			(nChInternal == 2 && lostFlag == FlagDecodeLBRR && channelState[1].lbrrFlags[channelState[1].nFramesDecoded] == 1)
	}
	// Call decoder for one frame.
	for n := 0; n < nChInternal; n++ {
		if n == 0 || hasSide {
			var condCoding int

			frameIndex := channelState[0].nFramesDecoded - n
			// Use independent coding if no previous frame available.
			if frameIndex <= 0 {
				condCoding = codeIndependently
			} else if lostFlag == FlagDecodeLBRR {
				if channelState[n].lbrrFlags[frameIndex-1] != 0 {
					condCoding = codeConditionally
				} else {
					condCoding = codeIndependently
				}
			} else if n > 0 && d.prevDecodeOnlyMiddle != 0 {
				// If we skipped a side frame in this packet, we don't need
				// LTP scaling; the LTP state is well-defined.
				condCoding = codeIndependentlyNoLTPScaling
			} else {
				condCoding = codeConditionally
			}
			var r int
			nSamplesOutDec, r = channelState[n].decodeFrame(
				psRangeDec,
				samplesOut1Tmp[n][2:],
				lostFlag,
				condCoding,
			)
			ret += r
		} else {
			clear(samplesOut1Tmp[n][2 : 2+nSamplesOutDec])
		}
		channelState[n].nFramesDecoded++
	}

	if nChAPI == 2 && nChInternal == 2 {
		// Convert Mid/Side to Left/Right.
		stereoMSToLR(
			&d.sStereo,
			samplesOut1Tmp[0],
			samplesOut1Tmp[1],
			&msPredQ13,
			channelState[0].fsKHz,
			nSamplesOutDec,
		)
	} else {
		// Buffering.
		copy(samplesOut1Tmp[0][:2], d.sStereo.sMid[:])
		copy(d.sStereo.sMid[:], samplesOut1Tmp[0][nSamplesOutDec:nSamplesOutDec+2])
	}

	// Number of output samples.
	nSamplesOut := div32(
		int32(nSamplesOutDec)*decControl.APISampleRate,
		smulbb(int32(channelState[0].fsKHz), 1000),
	)

	// Set up pointers to temp buffers.
	resampleOut := d.samplesOut2Tmp[:nSamplesOut]

	for n := 0; n < minInt(nChAPI, nChInternal); n++ {
		// Resample decoded signal to API_sampleRate.
		ret += resampler(
			&channelState[n].resamplerState,
			resampleOut,
			samplesOut1Tmp[n][1:1+nSamplesOutDec],
			nSamplesOutDec,
		)

		// Interleave if stereo output and stereo stream.
		if nChAPI == 2 {
			for i := 0; i < int(nSamplesOut); i++ {
				samplesOut[n+2*i] = resampleOut[i]
			}
		} else {
			copy(samplesOut[:nSamplesOut], resampleOut)
		}
	}

	// Create two channel output from mono stream.
	if nChAPI == 2 && nChInternal == 1 {
		if stereoToMono {
			// Resample right channel for newly collapsed stereo just in
			// case we weren't doing collapsing when switching to mono.
			ret += resampler(
				&channelState[1].resamplerState,
				resampleOut,
				samplesOut1Tmp[0][1:1+nSamplesOutDec],
				nSamplesOutDec,
			)

			for i := 0; i < int(nSamplesOut); i++ {
				samplesOut[1+2*i] = resampleOut[i]
			}
		} else {
			for i := 0; i < int(nSamplesOut); i++ {
				samplesOut[1+2*i] = samplesOut[0+2*i]
			}
		}
	}

	// Export pitch lag, measured at 48 kHz sampling rate.
	if channelState[0].prevSignalType == typeVoiced {
		multTab := [3]int{6, 4, 3}
		decControl.PrevPitchLag = channelState[0].lagPrev * multTab[(channelState[0].fsKHz-8)>>2]
	} else {
		decControl.PrevPitchLag = 0
	}

	if lostFlag == FlagPacketLost {
		// On packet loss, remove the gain clamping to prevent having the
		// energy "bounce back" if we lose packets when the energy is going
		// down.
		for i := 0; i < d.nChannelsInternal; i++ {
			d.channelState[i].lastGainIndex = 10
		}
	} else {
		d.prevDecodeOnlyMiddle = decodeOnlyMiddle
	}
	return nSamplesOut, ret
}

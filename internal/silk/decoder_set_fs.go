// Ported from libopus 1.6.1 silk/decoder_set_fs.c.

package silk

// decoderSetFs is silk_decoder_set_fs: set decoder sampling rate.
// preserve the reference prediction, gain and numerical decision sequence
//
//revive:disable-next-line:cognitive-complexity
func (psDec *decoderState) decoderSetFs(fsKHz int, fsAPIHz int32) int {
	ret := 0

	// New (sub)frame length.
	psDec.subfrLength = int(smulbb(subFrameLengthMs, int32(fsKHz)))
	frameLength := int(smulbb(int32(psDec.nbSubfr), int32(psDec.subfrLength)))

	// Initialize resampler when switching internal or external sampling
	// frequency.
	if psDec.fsKHz != fsKHz || psDec.fsAPIHz != fsAPIHz {
		// Initialize the resampler for dec_API.c preparing resampling from
		// fs_kHz to API_fs_Hz.
		ret += resamplerInit(&psDec.resamplerState, smulbb(int32(fsKHz), 1000), fsAPIHz, false)

		psDec.fsAPIHz = fsAPIHz
	}

	if psDec.fsKHz != fsKHz || frameLength != psDec.frameLength {
		if fsKHz == 8 {
			if psDec.nbSubfr == maxNbSubfr {
				psDec.pitchContourICDF = pitchContourNBICDF[:]
			} else {
				psDec.pitchContourICDF = pitchContour10MsNBICDF[:]
			}
		} else {
			if psDec.nbSubfr == maxNbSubfr {
				psDec.pitchContourICDF = pitchContourICDF[:]
			} else {
				psDec.pitchContourICDF = pitchContour10MsICDF[:]
			}
		}
		if psDec.fsKHz != fsKHz {
			psDec.ltpMemLength = int(smulbb(ltpMemLengthMs, int32(fsKHz)))
			if fsKHz == 8 || fsKHz == 12 {
				psDec.lpcOrder = minLPCOrder
				psDec.psNLSFCB = &nlsfCBNBMB
			} else {
				psDec.lpcOrder = maxLPCOrder
				psDec.psNLSFCB = &nlsfCBWB
			}
			switch fsKHz {
			case 16:
				psDec.pitchLagLowBitsICDF = uniform8ICDF[:]
			case 12:
				psDec.pitchLagLowBitsICDF = uniform6ICDF[:]
			case 8:
				psDec.pitchLagLowBitsICDF = uniform4ICDF[:]
			}
			psDec.firstFrameAfterReset = 1
			psDec.lagPrev = 100
			psDec.lastGainIndex = 10
			psDec.prevSignalType = typeNoVoiceActivity
			clear(psDec.outBuf[:])
			clear(psDec.sLPCQ14Buf[:])
		}

		psDec.fsKHz = fsKHz
		psDec.frameLength = frameLength
	}

	return ret
}

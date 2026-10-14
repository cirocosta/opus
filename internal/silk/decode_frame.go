// Ported from libopus 1.6.1 silk/decode_frame.c.

package silk

import "github.com/cirocosta/opus/internal/entcode"

// decodeFrame is silk_decode_frame: decode one frame into pOut and
// return its length.
func (psDec *decoderState) decodeFrame(
	psRangeDec *entcode.Decoder,
	pOut []int16,
	lostFlag int,
	condCoding int,
) (pN int, ret int) {
	psDecCtrl := &psDec.scratch.ctrl
	*psDecCtrl = decoderControl{}

	l := psDec.frameLength
	psDecCtrl.ltpScaleQ14 = 0

	if lostFlag == FlagDecodeNormal ||
		(lostFlag == FlagDecodeLBRR && psDec.lbrrFlags[psDec.nFramesDecoded] == 1) {
		pulses := psDec.scratch.pulses[:(l+shellCodecFrameLength-1)&^(shellCodecFrameLength-1)]

		// Decode quantization indices of side info.
		psDec.decodeIndices(psRangeDec, psDec.nFramesDecoded, lostFlag != 0, condCoding)

		// Decode quantization indices of excitation.
		decodePulses(psRangeDec, pulses, int(psDec.indices.signalType),
			int(psDec.indices.quantOffsetType), psDec.frameLength)

		// Decode parameters and pulse signal.
		psDec.decodeParameters(psDecCtrl, condCoding)

		// Run inverse NSQ.
		psDec.decodeCore(psDecCtrl, pOut, pulses)

		// Update output buffer.
		mvLen := psDec.ltpMemLength - psDec.frameLength
		copy(psDec.outBuf[:mvLen], psDec.outBuf[psDec.frameLength:psDec.frameLength+mvLen])
		copy(psDec.outBuf[mvLen:mvLen+psDec.frameLength], pOut[:psDec.frameLength])

		// Update PLC state.
		psDec.plc(psDecCtrl, pOut, false)

		psDec.lossCnt = 0
		psDec.prevSignalType = int(psDec.indices.signalType)

		// A frame has been decoded without errors.
		psDec.firstFrameAfterReset = 0
	} else {
		// Handle packet loss by extrapolation.
		psDec.plc(psDecCtrl, pOut, true)

		// Update output buffer.
		mvLen := psDec.ltpMemLength - psDec.frameLength
		copy(psDec.outBuf[:mvLen], psDec.outBuf[psDec.frameLength:psDec.frameLength+mvLen])
		copy(psDec.outBuf[mvLen:mvLen+psDec.frameLength], pOut[:psDec.frameLength])
	}

	// Comfort noise generation / estimation.
	psDec.cng(psDecCtrl, pOut, l)

	// Ensure smooth connection of extrapolated and good frames.
	psDec.plcGlueFrames(pOut, l)

	// Update some decoder state variables.
	psDec.lagPrev = psDecCtrl.pitchL[psDec.nbSubfr-1]

	// Set output frame length.
	return l, ret
}

// Ported from libopus 1.6.1 silk/float/LTP_scale_ctrl_FLP.c.

package silk

// ltpScaleCtrlFLP is silk_LTP_scale_ctrl_FLP.
func (psEnc *encoderStateFLP) ltpScaleCtrlFLP(psEncCtrl *encoderControlFLP, condCoding int) {
	if condCoding == codeIndependently {
		// Only scale if first frame in packet.
		roundLoss := psEnc.sCmn.packetLossPerc * psEnc.sCmn.nFramesPerPacket
		if psEnc.sCmn.lbrrFlag != 0 {
			// LBRR reduces the effective loss. In practice, it does not
			// square the loss because losses aren't independent, but that
			// still seems to work best. We also never go below 2%.
			roundLoss = 2 + int(smulbb(int32(roundLoss), int32(roundLoss)))/100
		}
		// silk_SMULBB( psEncCtrl->LTPredCodGain, round_loss ) converts the
		// float gain to opus_int32 (truncation) and then to opus_int16.
		g := int32(int16(int32(psEncCtrl.ltpredCodGain)))
		idx := 0
		if smulbb(g, int32(roundLoss)) > log2lin(2900-int32(psEnc.sCmn.snrDBQ7)) {
			idx = 1
		}
		if smulbb(g, int32(roundLoss)) > log2lin(3900-int32(psEnc.sCmn.snrDBQ7)) {
			idx++
		}
		psEnc.sCmn.indices.ltpScaleIndex = int8(idx)
	} else {
		// Default is minimum scaling.
		psEnc.sCmn.indices.ltpScaleIndex = 0
	}

	psEncCtrl.ltpScale = float32(ltpScalesTableQ14[psEnc.sCmn.indices.ltpScaleIndex]) / 16384.0
}

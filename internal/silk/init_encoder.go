// Ported from libopus 1.6.1 silk/init_encoder.c.

package silk

// initEncoder is silk_init_encoder: initialize the SILK encoder state.
func (psEnc *encoderStateFLP) initEncoder() int {
	ret := 0

	// Clear the entire encoder state.
	*psEnc = encoderStateFLP{}

	psEnc.sCmn.variableHPSmth1Q15 = variableHPSmthInitQ15
	psEnc.sCmn.variableHPSmth2Q15 = psEnc.sCmn.variableHPSmth1Q15

	// Used to deactivate LSF interpolation, pitch prediction.
	psEnc.sCmn.firstFrameAfterReset = 1

	// Initialize Silk VAD.
	ret += vadInit(&psEnc.sCmn.sVAD)

	return ret
}

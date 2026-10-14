// Read-only views of silk_encoder state that libopus 1.6.1
// src/opus_encoder.c reads directly from the struct.

package silk

// VariableHPSmth1Q15 returns state_Fxx[0].sCmn.variable_HP_smth1_Q15, the
// smoothed log pitch frequency that drives the Opus high-pass filter.
func (e *Encoder) VariableHPSmth1Q15() int32 {
	return e.stateFxx[0].sCmn.variableHPSmth1Q15
}

// NoSpeechCounter returns state_Fxx[n].sCmn.noSpeechCounter, the number
// of consecutive inactive frames of channel n.
func (e *Encoder) NoSpeechCounter(n int) int {
	return e.stateFxx[n].sCmn.noSpeechCounter
}

// PrevDecodeOnlyMiddle returns prev_decode_only_middle.
func (e *Encoder) PrevDecodeOnlyMiddle() int { return e.prevDecodeOnlyMiddle }

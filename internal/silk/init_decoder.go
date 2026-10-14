// Ported from libopus 1.6.1 silk/init_decoder.c.

package silk

// resetDecoder is silk_reset_decoder: clear the decoder state from
// SILK_DECODER_STATE_RESET_START (prev_gain_Q16) to the end.
func (psDec *decoderState) resetDecoder() int {
	psDec.decoderResetState = decoderResetState{}

	// Used to deactivate LSF interpolation.
	psDec.firstFrameAfterReset = 1
	psDec.prevGainQ16 = 65536

	// Reset CNG state.
	psDec.cngReset()

	// Reset PLC state.
	psDec.plcReset()

	return 0
}

// initDecoder is silk_init_decoder. The C code clears the whole state
// first; every field that carries state between calls is in
// decoderResetState, which resetDecoder clears.
func (psDec *decoderState) initDecoder() int {
	psDec.resetDecoder()
	return 0
}

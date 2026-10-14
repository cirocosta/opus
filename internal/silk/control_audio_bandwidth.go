// Ported from libopus 1.6.1 silk/control_audio_bandwidth.c.

package silk

// controlAudioBandwidth is silk_control_audio_bandwidth: control the
// internal sampling rate. It returns the internal sampling rate in kHz.
func (psEncC *encoderState) controlAudioBandwidth(encControl *EncControl) int {
	origKHz := psEncC.fsKHz
	// Handle a bandwidth-switching reset where we need to be aware what
	// the last sampling rate was.
	if origKHz == 0 {
		origKHz = int(psEncC.sLP.savedFsKHz)
	}
	fsKHz := origKHz
	fsHz := int(smulbb(int32(fsKHz), 1000))
	if fsHz == 0 {
		// Encoder has just been initialized.
		fsHz = minInt(psEncC.desiredInternalFsHz, int(psEncC.apiFsHz))
		fsKHz = fsHz / 1000
	} else if fsHz > int(psEncC.apiFsHz) || fsHz > psEncC.maxInternalFsHz || fsHz < psEncC.minInternalFsHz {
		// Make sure internal rate is not higher than external rate or
		// maximum allowed, or lower than minimum allowed.
		fsHz = int(psEncC.apiFsHz)
		fsHz = minInt(fsHz, psEncC.maxInternalFsHz)
		fsHz = maxInt(fsHz, psEncC.minInternalFsHz)
		fsKHz = fsHz / 1000
	} else {
		// State machine for the internal sampling rate switching.
		if psEncC.sLP.transitionFrameNo >= transitionFrames {
			// Stop transition phase.
			psEncC.sLP.mode = 0
		}
		if psEncC.allowBandwidthSwitch != 0 || encControl.OpusCanSwitch != 0 {
			// Check if we should switch down.
			if int(smulbb(int32(origKHz), 1000)) > psEncC.desiredInternalFsHz {
				// Switch down.
				if psEncC.sLP.mode == 0 {
					// New transition.
					psEncC.sLP.transitionFrameNo = transitionFrames

					// Reset transition filter state.
					psEncC.sLP.inLPState = [2]int32{}
				}
				if encControl.OpusCanSwitch != 0 {
					// Stop transition phase.
					psEncC.sLP.mode = 0

					// Switch to a lower sample frequency.
					if origKHz == 16 {
						fsKHz = 12
					} else {
						fsKHz = 8
					}
				} else {
					if psEncC.sLP.transitionFrameNo <= 0 {
						encControl.SwitchReady = 1
						// Make room for redundancy.
						encControl.MaxBits -= encControl.MaxBits * 5 / (encControl.PayloadSizeMs + 5)
					} else {
						// Direction: down (at double speed).
						psEncC.sLP.mode = -2
					}
				}
			} else if int(smulbb(int32(origKHz), 1000)) < psEncC.desiredInternalFsHz {
				// Check if we should switch up.
				if encControl.OpusCanSwitch != 0 {
					// Switch to a higher sample frequency.
					if origKHz == 8 {
						fsKHz = 12
					} else {
						fsKHz = 16
					}

					// New transition.
					psEncC.sLP.transitionFrameNo = 0

					// Reset transition filter state.
					psEncC.sLP.inLPState = [2]int32{}

					// Direction: up.
					psEncC.sLP.mode = 1
				} else {
					if psEncC.sLP.mode == 0 {
						encControl.SwitchReady = 1
						// Make room for redundancy.
						encControl.MaxBits -= encControl.MaxBits * 5 / (encControl.PayloadSizeMs + 5)
					} else {
						// Direction: up.
						psEncC.sLP.mode = 1
					}
				}
			} else {
				if psEncC.sLP.mode < 0 {
					psEncC.sLP.mode = 1
				}
			}
		}
	}

	return fsKHz
}

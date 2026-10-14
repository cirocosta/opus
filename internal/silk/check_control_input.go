// Ported from libopus 1.6.1 silk/check_control_input.c.

package silk

// checkControlInput is check_control_input: check encoder control
// struct.
func checkControlInput(encControl *EncControl) int {
	if (encControl.APISampleRate != 8000 &&
		encControl.APISampleRate != 12000 &&
		encControl.APISampleRate != 16000 &&
		encControl.APISampleRate != 24000 &&
		encControl.APISampleRate != 32000 &&
		encControl.APISampleRate != 44100 &&
		encControl.APISampleRate != 48000) ||
		(encControl.DesiredInternalSampleRate != 8000 &&
			encControl.DesiredInternalSampleRate != 12000 &&
			encControl.DesiredInternalSampleRate != 16000) ||
		(encControl.MaxInternalSampleRate != 8000 &&
			encControl.MaxInternalSampleRate != 12000 &&
			encControl.MaxInternalSampleRate != 16000) ||
		(encControl.MinInternalSampleRate != 8000 &&
			encControl.MinInternalSampleRate != 12000 &&
			encControl.MinInternalSampleRate != 16000) ||
		encControl.MinInternalSampleRate > encControl.DesiredInternalSampleRate ||
		encControl.MaxInternalSampleRate < encControl.DesiredInternalSampleRate ||
		encControl.MinInternalSampleRate > encControl.MaxInternalSampleRate {
		return EncFsNotSupported
	}
	if encControl.PayloadSizeMs != 10 &&
		encControl.PayloadSizeMs != 20 &&
		encControl.PayloadSizeMs != 40 &&
		encControl.PayloadSizeMs != 60 {
		return EncPacketSizeNotSupported
	}
	if encControl.PacketLossPercentage < 0 || encControl.PacketLossPercentage > 100 {
		return EncInvalidLossRate
	}
	if encControl.UseDTX < 0 || encControl.UseDTX > 1 {
		return EncInvalidDTXSetting
	}
	if encControl.UseCBR < 0 || encControl.UseCBR > 1 {
		return EncInvalidCBRSetting
	}
	if encControl.UseInBandFEC < 0 || encControl.UseInBandFEC > 1 {
		return EncInvalidInbandFECSetting
	}
	if encControl.NChannelsAPI < 1 || encControl.NChannelsAPI > encoderNumChannels {
		return EncInvalidNumberOfChannelsError
	}
	if encControl.NChannelsInternal < 1 || encControl.NChannelsInternal > encoderNumChannels {
		return EncInvalidNumberOfChannelsError
	}
	if encControl.NChannelsInternal > encControl.NChannelsAPI {
		return EncInvalidNumberOfChannelsError
	}
	if encControl.Complexity < 0 || encControl.Complexity > 10 {
		return EncInvalidComplexitySetting
	}

	return NoError
}

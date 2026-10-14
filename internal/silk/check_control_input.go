// Ported from libopus 1.6.1 silk/check_control_input.c.

package silk

// checkControlInput validates settings in the reference's error priority order.
func checkControlInput(control *EncControl) int {
	if !validSampleRateConfiguration(control) {
		return EncFsNotSupported
	}
	switch control.PayloadSizeMs {
	case 10, 20, 40, 60:
	default:
		return EncPacketSizeNotSupported
	}
	checks := [...]struct {
		value, low, high, code int
	}{
		{control.PacketLossPercentage, 0, 100, EncInvalidLossRate},
		{control.UseDTX, 0, 1, EncInvalidDTXSetting},
		{control.UseCBR, 0, 1, EncInvalidCBRSetting},
		{control.UseInBandFEC, 0, 1, EncInvalidInbandFECSetting},
		{int(control.NChannelsAPI), 1, encoderNumChannels, EncInvalidNumberOfChannelsError},
		{int(control.NChannelsInternal), 1, encoderNumChannels, EncInvalidNumberOfChannelsError},
	}
	for _, check := range checks {
		if check.value < check.low || check.value > check.high {
			return check.code
		}
	}
	if control.NChannelsInternal > control.NChannelsAPI {
		return EncInvalidNumberOfChannelsError
	}
	if control.Complexity < 0 || control.Complexity > 10 {
		return EncInvalidComplexitySetting
	}
	return NoError
}

func validSampleRateConfiguration(control *EncControl) bool {
	switch control.APISampleRate {
	case 8000, 12000, 16000, 24000, 32000, 44100, 48000:
	default:
		return false
	}
	for _, rate := range [...]int32{
		control.DesiredInternalSampleRate,
		control.MinInternalSampleRate,
		control.MaxInternalSampleRate,
	} {
		switch rate {
		case 8000, 12000, 16000:
		default:
			return false
		}
	}
	return control.MinInternalSampleRate <= control.DesiredInternalSampleRate &&
		control.DesiredInternalSampleRate <= control.MaxInternalSampleRate
}

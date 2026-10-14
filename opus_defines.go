// Ported from libopus 1.6.1 include/opus_defines.h, plus opus_strerror and
// opus_get_version_string from celt/celt.c.

package opus

import "strconv"

// OK is the success return code (OPUS_OK).
const OK = 0

// Error is a libopus error code. Its value is the negative numeric code that
// libopus returns (for example, ErrBadArg is -1).
type Error int

// Error codes from opus_defines.h.
const (
	ErrBadArg         Error = -1 // OPUS_BAD_ARG: one or more invalid or out of range arguments.
	ErrBufferTooSmall Error = -2 // OPUS_BUFFER_TOO_SMALL: not enough bytes allocated in the buffer.
	ErrInternalError  Error = -3 // OPUS_INTERNAL_ERROR: an internal error was detected.
	ErrInvalidPacket  Error = -4 // OPUS_INVALID_PACKET: the compressed data passed is corrupted.
	ErrUnimplemented  Error = -5 // OPUS_UNIMPLEMENTED: invalid or unsupported request number.
	ErrInvalidState   Error = -6 // OPUS_INVALID_STATE: an encoder or decoder structure is invalid or already freed.
	ErrAllocFail      Error = -7 // OPUS_ALLOC_FAIL: memory allocation has failed.
)

// Code returns the numeric libopus error code.
func (e Error) Code() int { return int(e) }

// Error returns the opus_strerror message for the code, prefixed with
// "opus: ".
func (e Error) Error() string {
	if e > 0 || e < -7 {
		return "opus: unknown error " + strconv.Itoa(int(e))
	}
	return "opus: " + Strerror(int(e))
}

// errorFromCode converts a libopus return code into a Go error. Codes that
// are not negative mean success and return nil.
func errorFromCode(code int) error {
	if code >= 0 {
		return nil
	}
	return Error(code)
}

// Values for the encoder and decoder controls.
const (
	Auto       = -1000 // OPUS_AUTO: auto or default setting.
	BitrateMax = -1    // OPUS_BITRATE_MAX: maximum bitrate.
)

// Application modes.
const (
	ApplicationVoIP               = 2048 // OPUS_APPLICATION_VOIP
	ApplicationAudio              = 2049 // OPUS_APPLICATION_AUDIO
	ApplicationRestrictedLowdelay = 2051 // OPUS_APPLICATION_RESTRICTED_LOWDELAY
	ApplicationRestrictedSilk     = 2052 // OPUS_APPLICATION_RESTRICTED_SILK
	ApplicationRestrictedCelt     = 2053 // OPUS_APPLICATION_RESTRICTED_CELT
)

// Signal types.
const (
	SignalVoice = 3001 // OPUS_SIGNAL_VOICE
	SignalMusic = 3002 // OPUS_SIGNAL_MUSIC
)

// Audio bandwidths.
const (
	BandwidthNarrowband    = 1101 // OPUS_BANDWIDTH_NARROWBAND: 4 kHz bandpass.
	BandwidthMediumband    = 1102 // OPUS_BANDWIDTH_MEDIUMBAND: 6 kHz bandpass.
	BandwidthWideband      = 1103 // OPUS_BANDWIDTH_WIDEBAND: 8 kHz bandpass.
	BandwidthSuperwideband = 1104 // OPUS_BANDWIDTH_SUPERWIDEBAND: 12 kHz bandpass.
	BandwidthFullband      = 1105 // OPUS_BANDWIDTH_FULLBAND: 20 kHz bandpass.
)

// Frame size selections for the expert frame duration control.
const (
	FramesizeArg   = 5000 // OPUS_FRAMESIZE_ARG: select frame size from the argument.
	Framesize2_5ms = 5001 // OPUS_FRAMESIZE_2_5_MS
	Framesize5ms   = 5002 // OPUS_FRAMESIZE_5_MS
	Framesize10ms  = 5003 // OPUS_FRAMESIZE_10_MS
	Framesize20ms  = 5004 // OPUS_FRAMESIZE_20_MS
	Framesize40ms  = 5005 // OPUS_FRAMESIZE_40_MS
	Framesize60ms  = 5006 // OPUS_FRAMESIZE_60_MS
	Framesize80ms  = 5007 // OPUS_FRAMESIZE_80_MS
	Framesize100ms = 5008 // OPUS_FRAMESIZE_100_MS
	Framesize120ms = 5009 // OPUS_FRAMESIZE_120_MS
)

// Strerror returns the libopus message for an error code. It ports
// opus_strerror.
func Strerror(code int) string {
	errorStrings := [8]string{
		"success",
		"invalid argument",
		"buffer too small",
		"internal error",
		"corrupted stream",
		"request not implemented",
		"invalid state",
		"memory allocation failed",
	}
	if code > 0 || code < -7 {
		return "unknown error"
	}
	return errorStrings[-code]
}

// VersionString returns the libopus version string that this package
// ports. It ports opus_get_version_string for the floating-point build.
func VersionString() string {
	return "libopus 1.6.1"
}

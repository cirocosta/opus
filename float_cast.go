// Ported from libopus 1.6.1 celt/float_cast.h and the float-build
// conversion macros of celt/arch.h.

package opus

import "math"

// float2int ports float2int (lrintf): round to nearest, ties to even, as a
// long, then truncate to opus_int32.
func float2int(x float32) int32 {
	return int32(int64(math.RoundToEven(float64(x))))
}

// float2int16 ports FLOAT2INT16.
func float2int16(x float32) int16 {
	x = x * 32768
	if !(x > -32768) {
		x = -32768
	}
	if !(x < 32767) {
		x = 32767
	}
	return int16(float2int(x))
}

// res2int16 ports RES2INT16 for the float build (opus_res is float).
func res2int16(a float32) int16 { return float2int16(a) }

// res2int24 ports RES2INT24 for the float build.
func res2int24(a float32) int32 { return float2int(32768 * 256 * a) }

// int24ToRes ports INT24TORES for the float build.
func int24ToRes(a float32) float32 { return (1.0 / 32768 / 256) * a }

// Ported from libopus 1.6.1 celt/float_cast.h.

package dsp

import "math"

// Float2Int is float2int. The x86-64 reference build uses cvtss2si, which
// rounds to nearest even and returns INT32_MIN for NaN and for values out
// of the int32 range. This function does the same on every platform.
func Float2Int(x float32) int32 {
	r := math.RoundToEven(float64(x))
	if !(r >= -2147483648 && r < 2147483648) {
		return math.MinInt32
	}
	return int32(r)
}

// Float2Int16 is FLOAT2INT16: it scales x by CELT_SIG_SCALE, clamps it to
// [-32768, 32767] and rounds it to nearest. NaN gives -32768, as in C.
func Float2Int16(x float32) int16 {
	x = float32(x * CeltSigScale)
	x = Max32(x, -32768)
	x = Min32(x, 32767)
	return int16(Float2Int(x))
}

// Float2Int24 is FLOAT2INT24.
func Float2Int24(x float32) int32 {
	x = float32(x * (CeltSigScale * 256))
	x = Max32(x, -16777216)
	x = Min32(x, 16777216)
	return Float2Int(x)
}

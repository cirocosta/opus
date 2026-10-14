// Ported from libopus 1.6.1 celt/arch.h (float build).

// Package dsp ports the CELT signal processing primitives of libopus 1.6.1
// (floating-point build): the KISS FFT, the MDCT, LPC helpers, the pitch
// analysis, the float math helpers, float_cast.h, and the precomputed tables
// of the static 48 kHz / 960-sample mode.
//
// # Arithmetic
//
// In the float build the fixed-point macros of celt/arch.h reduce to plain
// float operations. The port expands them in place:
//
//   - MULT16_16, MULT16_32_Q15, MULT32_32_Q31, MULT16_16_Q15, S_MUL and
//     the other MULT* macros are a*b.
//   - MAC16_16(c,a,b) and MAC16_32_Q15(c,a,b) are c + a*b.
//   - ADD32, SUB32, ADD16, SUB16 and the _ovflw forms are a+b and a-b.
//   - SHL32, SHR32, PSHR32, VSHR32, ROUND16, SROUND16, EXTRACT16,
//     EXTEND32, QCONST16, QCONST32, COEF2VAL16 and SATURATE are the
//     identity.
//   - HALF16 and HALF32 are .5f*x.
//   - DIV32 and DIV32_16 are a/b.
//   - MIN16, MAX16, MIN32, MAX32 are C ternaries; see [Min32] and [Max32].
//   - ABS16 and ABS32 are (float)fabs(x).
//
// All values are float32, as in C. The Go specification allows the
// compiler to fuse x*y+z into one FMA (it does so on arm64, and on amd64
// with GOAMD64=v3). libopus is built with -ffp-contract=off, so every
// product that feeds an addition or subtraction is wrapped in an explicit
// float32 conversion, which forces the rounding and prevents the fusion.
package dsp

import "math"

// Constants from celt/arch.h (float build) and celt/mathops.h.
const (
	// CeltSigScale is CELT_SIG_SCALE.
	CeltSigScale = float32(32768.0)
	// Epsilon is EPSILON.
	Epsilon = float32(1e-15)
	// VerySmall is VERY_SMALL.
	VerySmall = float32(1e-30)
	// VeryLarge16 is VERY_LARGE16.
	VeryLarge16 = float32(1e15)
	// Q15One is Q15ONE.
	Q15One = float32(1.0)
	// Pi is PI from celt/mathops.h (a double).
	Pi = 3.1415926535897931
)

// Min16 is MIN16: a < b ? a : b. Unlike the Go builtin min, it keeps the C
// behavior for NaN and signed zeros.
func Min16(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

// Max16 is MAX16: a > b ? a : b.
func Max16(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

// Min32 is MIN32: a < b ? a : b.
func Min32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

// Max32 is MAX32: a > b ? a : b.
func Max32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

// Abs16 is ABS16: (float)fabs(x).
func Abs16(x float32) float32 {
	return float32(math.Abs(float64(x)))
}

// Abs32 is ABS32: (float)fabs(x).
func Abs32(x float32) float32 {
	return float32(math.Abs(float64(x)))
}

// IMin is IMIN.
func IMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// IMax is IMAX.
func IMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Sample format conversions from celt/arch.h (float build). opus_res and
// celt_sig are float32; opus_res is normalized to +/-1 and celt_sig to
// +/-32768.

// Sig2Res is SIG2RES: (1/CELT_SIG_SCALE)*a.
func Sig2Res(a float32) float32 { return float32((1 / CeltSigScale) * a) }

// Res2Int16 is RES2INT16: FLOAT2INT16(a).
func Res2Int16(a float32) int16 { return Float2Int16(a) }

// Res2Int24 is RES2INT24: float2int(32768.f*256.f*a).
func Res2Int24(a float32) int32 { return Float2Int(float32(32768 * 256 * a)) }

// Int16ToRes is INT16TORES: a*(1/CELT_SIG_SCALE).
func Int16ToRes(a int16) float32 { return float32(float32(a) * (1 / CeltSigScale)) }

// Int24ToRes is INT24TORES: (1.f/32768.f/256.f)*a.
func Int24ToRes(a int32) float32 { return float32((1.0 / 32768.0 / 256.0) * float32(a)) }

// Res2Sig is RES2SIG: CELT_SIG_SCALE*a.
func Res2Sig(a float32) float32 { return float32(CeltSigScale * a) }

// Float2Sig is FLOAT2SIG: a*CELT_SIG_SCALE.
func Float2Sig(a float32) float32 { return float32(a * CeltSigScale) }

// Int16ToSig is INT16TOSIG: (float)a.
func Int16ToSig(a int16) float32 { return float32(a) }

// Int24ToSig is INT24TOSIG: (float)a*(1.f/256.f).
func Int24ToSig(a int32) float32 { return float32(float32(a) * (1.0 / 256.0)) }

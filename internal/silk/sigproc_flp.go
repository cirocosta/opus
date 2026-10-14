// Ported from libopus 1.6.1 silk/float/SigProc_FLP.h.

package silk

import (
	"math"

	"github.com/cirocosta/opus/internal/dsp"
)

// piFLP is PI in SigProc_FLP.h.
const piFLP = float32(3.1415926536)

// minFloat is silk_min_float.
func minFloat(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

// maxFloat is silk_max_float.
func maxFloat(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

// absFloat is silk_abs_float: (silk_float)fabs(a).
func absFloat(a float32) float32 {
	return float32(math.Abs(float64(a)))
}

// sigmoid is silk_sigmoid: (silk_float)(1.0 / (1.0 + exp(-x))), in
// double precision.
func sigmoid(x float32) float32 {
	return float32(1.0 / (1.0 + math.Exp(float64(-x))))
}

// float2intFLP is silk_float2int: floating-point to integer conversion
// with rounding (float2int).
func float2intFLP(x float32) int32 {
	return dsp.Float2Int(x)
}

// float2shortArray is silk_float2short_array.
func float2shortArray(out []int16, in []float32, length int) {
	for k := length - 1; k >= 0; k-- {
		out[k] = int16(sat16(dsp.Float2Int(in[k])))
	}
}

// short2floatArray is silk_short2float_array.
func short2floatArray(out []float32, in []int16, length int) {
	for k := length - 1; k >= 0; k-- {
		out[k] = float32(in[k])
	}
}

// silkLog2 is silk_log2: ( silk_float )( 3.32192809488736 * log10( x ) ).
func silkLog2(x float64) float32 {
	return float32(3.32192809488736 * math.Log10(x))
}

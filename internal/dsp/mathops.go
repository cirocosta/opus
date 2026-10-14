// Ported from libopus 1.6.1 celt/mathops.h and celt/mathops.c (float build).

package dsp

import (
	"math"
	"math/bits"
)

// FloatApprox selects the FLOAT_APPROX variants of CeltLog2 and CeltExp2.
//
// PORTING.md targets a build without FLOAT_APPROX, so the default uses
// libm (log and exp in double precision). Note that the configure script
// of libopus 1.6.1 enables FLOAT_APPROX by default on x86, and the scalar
// reference build defines it in config.h. Set this to true to match that
// build bit for bit. Both variants are always available as
// [CeltLog2Libm], [CeltExp2Libm], [CeltLog2Approx] and [CeltExp2Approx].
const FloatApprox = true

// Isqrt32 is isqrt32: floor(sqrt(val)) with exact arithmetic. val must be
// greater than 0.
func Isqrt32(val uint32) uint32 {
	var g uint32
	bshift := (ecIlog(val) - 1) >> 1
	b := uint32(1) << uint(bshift)
	for {
		t := ((g << 1) + b) << uint(bshift)
		if t <= val {
			g += b
			val -= t
		}
		b >>= 1
		bshift--
		if bshift < 0 {
			break
		}
	}
	return g
}

// ecIlog is EC_ILOG: the number of bits needed to represent x (0 for 0).
func ecIlog(x uint32) int {
	return bits.Len32(x)
}

// FracMul16 is FRAC_MUL16: multiplies two 16-bit fractional values. The
// arguments are truncated to int16 as in C.
func FracMul16(a, b int32) int32 {
	return (16384 + int32(int16(a))*int32(int16(b))) >> 15
}

// FastAtan2f is fast_atan2f.
func FastAtan2f(y, x float32) float32 {
	const (
		cA = float32(0.43157974)
		cB = float32(0.67848403)
		cC = float32(0.08595542)
	)
	cE := float32(Pi) / 2
	x2 := float32(x * x)
	y2 := float32(y * y)
	// For very small values, we don't care about the answer, so we can
	// just return 0.
	if x2+y2 < 1e-18 {
		return 0
	}
	if x2 < y2 {
		den := (y2 + float32(cB*x2)) * (y2 + float32(cC*x2))
		e := cE
		if y < 0 {
			e = -cE
		}
		return float32(float32(-x*y)*(y2+float32(cA*x2))/den) + e
	}
	den := (x2 + float32(cB*y2)) * (x2 + float32(cC*y2))
	e1 := cE
	if y < 0 {
		e1 = -cE
	}
	e2 := cE
	if float32(x*y) < 0 {
		e2 = -cE
	}
	return float32(float32(x*y)*(x2+float32(cA*y2))/den) + e1 - e2
}

// CeltMaxabs16 is celt_maxabs16 over all of x.
func CeltMaxabs16(x []float32) float32 {
	var maxval, minval float32
	for _, v := range x {
		maxval = Max16(maxval, v)
		minval = Min16(minval, v)
	}
	return Max32(maxval, -minval)
}

// CeltMaxabs32 is celt_maxabs32, which is celt_maxabs16 in the float build.
func CeltMaxabs32(x []float32) float32 {
	return CeltMaxabs16(x)
}

// CeltMaxabsRes is celt_maxabs_res, which is celt_maxabs16 in the float
// build.
func CeltMaxabsRes(x []float32) float32 {
	return CeltMaxabs16(x)
}

// CeltAtanNorm is celt_atan_norm: atan(x)*2/pi with an order 15 Remez
// polynomial.
func CeltAtanNorm(x float32) float32 {
	const (
		atan2TwoOverPi = float32(0.636619772367581)
		a03            = float32(-3.3331659436225891113281250000e-01)
		a05            = float32(1.99627041816711425781250000000e-01)
		a07            = float32(-1.3976582884788513183593750000e-01)
		a09            = float32(9.79423448443412780761718750000e-02)
		a11            = float32(-5.7773590087890625000000000000e-02)
		a13            = float32(2.30401363223791122436523437500e-02)
		a15            = float32(-4.3554059229791164398193359375e-03)
	)
	xSq := float32(x * x)
	p := a13 + float32(xSq*a15)
	p = a11 + float32(xSq*p)
	p = a09 + float32(xSq*p)
	p = a07 + float32(xSq*p)
	p = a05 + float32(xSq*p)
	p = a03 + float32(xSq*p)
	return float32(atan2TwoOverPi * (x + float32(float32(x*xSq)*p)))
}

// CeltAtan2pNorm is celt_atan2p_norm: atan2(y, x)*2/pi for x, y >= 0.
func CeltAtan2pNorm(y, x float32) float32 {
	// For very small values, we don't care about the answer.
	if float32(x*x)+float32(y*y) < 1e-18 {
		return 0
	}
	if y < x {
		return CeltAtanNorm(y / x)
	}
	return 1 - CeltAtanNorm(x/y)
}

// CeltCosNorm2 is celt_cos_norm2: cos(pi/2*x) with an even polynomial.
func CeltCosNorm2(x float32) float32 {
	const (
		a0 = float32(9.999999403953552246093750000000e-01)
		a2 = float32(-1.233698248863220214843750000000000)
		a4 = float32(2.536507546901702880859375000000e-01)
		a6 = float32(-2.08106283098459243774414062500e-02)
		a8 = float32(8.581906440667808055877685546875e-04)
	)
	// Restrict x to [-1, 3].
	x = float32(float64(x) - float64(4*math.Floor(.25*float64(x+1))))
	// Negative sign for [1, 3].
	outputSign := float32(1)
	if x > 1 {
		outputSign = -1
		// Restrict to [-1, 1].
		x -= 2
	}
	xNormSq := float32(x * x)
	p := a6 + float32(xNormSq*a8)
	p = a4 + float32(xNormSq*p)
	p = a2 + float32(xNormSq*p)
	p = a0 + float32(xNormSq*p)
	return float32(outputSign * p)
}

// CeltSqrt is celt_sqrt: (float)sqrt(x).
func CeltSqrt(x float32) float32 {
	return float32(math.Sqrt(float64(x)))
}

// CeltSqrt32 is celt_sqrt32.
func CeltSqrt32(x float32) float32 {
	return float32(math.Sqrt(float64(x)))
}

// CeltRsqrt is celt_rsqrt: 1.f/celt_sqrt(x).
func CeltRsqrt(x float32) float32 {
	return 1 / CeltSqrt(x)
}

// CeltRsqrtNorm is celt_rsqrt_norm.
func CeltRsqrtNorm(x float32) float32 {
	return CeltRsqrt(x)
}

// CeltRsqrtNorm32 is celt_rsqrt_norm32.
func CeltRsqrtNorm32(x float32) float32 {
	return CeltRsqrt(x)
}

// CeltCosNorm is celt_cos_norm: (float)cos((.5f*PI)*x).
func CeltCosNorm(x float32) float32 {
	return float32(math.Cos(float64((.5 * Pi) * float64(x))))
}

// CeltRcp is celt_rcp: 1.f/x.
func CeltRcp(x float32) float32 {
	return 1 / x
}

// CeltDiv is celt_div: a/b.
func CeltDiv(a, b float32) float32 {
	return a / b
}

// FracDiv32 is frac_div32 (and frac_div32_q29): a/b.
func FracDiv32(a, b float32) float32 {
	return a / b
}

// CeltLog2 is celt_log2 (and celt_log2_db). See [FloatApprox].
func CeltLog2(x float32) float32 {
	if FloatApprox {
		return CeltLog2Approx(x)
	}
	return CeltLog2Libm(x)
}

// CeltExp2 is celt_exp2 (and celt_exp2_db). See [FloatApprox].
func CeltExp2(x float32) float32 {
	if FloatApprox {
		return CeltExp2Approx(x)
	}
	return CeltExp2Libm(x)
}

// CeltLog2Libm is celt_log2 without FLOAT_APPROX:
// (float)(1.442695040888963387*log(x)).
func CeltLog2Libm(x float32) float32 {
	return float32(1.442695040888963387 * math.Log(float64(x)))
}

// CeltExp2Libm is celt_exp2 without FLOAT_APPROX:
// (float)exp(0.6931471805599453094*x).
func CeltExp2Libm(x float32) float32 {
	return float32(math.Exp(float64(0.6931471805599453094 * float64(x))))
}

var log2XNormCoeff = [8]float32{
	1.000000000000000000000000000, 8.88888895511627197265625e-01,
	8.00000000000000000000000e-01, 7.27272748947143554687500e-01,
	6.66666686534881591796875e-01, 6.15384638309478759765625e-01,
	5.71428596973419189453125e-01, 5.33333361148834228515625e-01,
}

var log2YNormCoeff = [8]float32{
	0.0000000000000000000000000000, 1.699250042438507080078125e-01,
	3.219280838966369628906250e-01, 4.594316184520721435546875e-01,
	5.849624872207641601562500e-01, 7.004396915435791015625000e-01,
	8.073549270629882812500000e-01, 9.068905711174011230468750e-01,
}

// CeltLog2Approx is celt_log2 with FLOAT_APPROX. Denormals, infinities and
// NaN are not handled, as in C.
func CeltLog2Approx(x float32) float32 {
	const (
		a0 = float32(8.74628424644470214843750000e-02)
		a1 = float32(1.357829570770263671875000000000)
		a2 = float32(-6.3897705078125000000000000e-01)
		a3 = float32(4.01971250772476196289062500e-01)
		a4 = float32(-2.8415444493293762207031250e-01)
	)
	in := math.Float32bits(x)
	integer := int32(in>>23) - 127
	in = uint32(int32(in) - int32(uint32(integer)<<23))
	// Normalize the mantissa range from [1, 2] to [1,1.125], and then
	// shift x by 1.0625 to [-0.0625, 0.0625].
	rangeIdx := (in >> 20) & 0x7
	f := float32(math.Float32frombits(in)*log2XNormCoeff[rangeIdx]) - 1.0625
	p := a3 + float32(f*a4)
	p = a2 + float32(f*p)
	p = a1 + float32(f*p)
	f = a0 + float32(f*p)
	return float32(integer) + f + log2YNormCoeff[rangeIdx]
}

// CeltExp2Approx is celt_exp2 with FLOAT_APPROX.
func CeltExp2Approx(x float32) float32 {
	const (
		a0 = float32(9.999999403953552246093750000000e-01)
		a1 = float32(6.931530833244323730468750000000e-01)
		a2 = float32(2.401536107063293457031250000000e-01)
		a3 = float32(5.582631751894950866699218750000e-02)
		a4 = float32(8.989339694380760192871093750000e-03)
		a5 = float32(1.877576694823801517486572265625e-03)
	)
	// (int)floor(x) is cvttsd2si on x86-64: INT32_MIN when out of range.
	fl := math.Floor(float64(x))
	integer := int32(math.MinInt32)
	if fl >= -2147483648 && fl < 2147483648 {
		integer = int32(fl)
	}
	if integer < -50 {
		return 0
	}
	frac := x - float32(integer)
	p := a4 + float32(frac*a5)
	p = a3 + float32(frac*p)
	p = a2 + float32(frac*p)
	p = a1 + float32(frac*p)
	p = a0 + float32(frac*p)
	res := math.Float32bits(p)
	res = uint32(int32(res)+int32(uint32(integer)<<23)) & 0x7fffffff
	return math.Float32frombits(res)
}

// CeltLog2Db is celt_log2_db, which is celt_log2 in the float build.
func CeltLog2Db(x float32) float32 {
	return CeltLog2(x)
}

// CeltExp2Db is celt_exp2_db, which is celt_exp2 in the float build.
func CeltExp2Db(x float32) float32 {
	return CeltExp2(x)
}

// CeltSin is celt_sin: celt_cos_norm2((0.5f*PI)*x - 1.0f).
func CeltSin(x float32) float32 {
	return CeltCosNorm2(float32(float64((0.5*Pi)*float64(x)) - 1.0))
}

// CeltLog is celt_log: celt_log2(x)*0.6931471805599453f.
func CeltLog(x float32) float32 {
	return float32(CeltLog2(x) * float32(0.6931471805599453))
}

// CeltExp is celt_exp: celt_exp2(x*1.4426950408889634f).
func CeltExp(x float32) float32 {
	return CeltExp2(float32(x * float32(1.4426950408889634)))
}

// CeltIsnan is celt_isnan. The FLOAT_APPROX and libm versions agree.
func CeltIsnan(x float32) bool {
	return x != x
}

// CeltFloat2Int16 is celt_float2int16_c over min(len(in), len(out))
// samples.
func CeltFloat2Int16(in []float32, out []int16) {
	out = out[:len(in)]
	for i := range in {
		out[i] = Float2Int16(in[i])
	}
}

// OpusLimit2Checkwithin1 is opus_limit2_checkwithin1_c: it clamps the
// samples to [-2, 2] and returns 1 only when there are no samples.
func OpusLimit2Checkwithin1(samples []float32) int {
	if len(samples) == 0 {
		return 1
	}
	for i, v := range samples {
		// FMAX(-2.0f, v), then FMIN(2.0f, v).
		if -2 > v {
			v = -2
		}
		if 2 < v {
			v = 2
		}
		samples[i] = v
	}
	// C implementation can't provide quick hint. Assume it might exceed
	// -1/+1.
	return 0
}

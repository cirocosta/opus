// Ported from libopus 1.6.1 celt/celt.h and celt/celt.c (float build, no
// QEXT, no CUSTOM_MODES).

package celt

import (
	"math"

	"github.com/cirocosta/opus/internal/entcode"
)

// Return codes from include/opus_defines.h, as returned by the C decoder
// functions and CTLs.
const (
	OK             = 0
	BadArg         = -1
	BufferTooSmall = -2
	InternalError  = -3
	InvalidPacket  = -4
	Unimplemented  = -5
	InvalidState   = -6
	AllocFail      = -7
)

// Constants from celt/celt.h, celt/bands.h and celt/rate.h.
const (
	// LeakBands is LEAK_BANDS.
	LeakBands = 19

	// CombFilterMaxPeriod is COMBFILTER_MAXPERIOD.
	CombFilterMaxPeriod = 1024
	// CombFilterMinPeriod is COMBFILTER_MINPERIOD.
	CombFilterMinPeriod = 15

	// Spreading modes (SPREAD_NONE and friends).
	spreadNone       = 0
	spreadLight      = 1
	spreadNormal     = 2
	spreadAggressive = 3

	maxPseudo            = 40 // MAX_PSEUDO
	logMaxPseudo         = 6  // LOG_MAX_PSEUDO
	celtMaxPulses        = 128
	maxFineBits          = 8  // MAX_FINE_BITS
	fineOffset           = 21 // FINE_OFFSET
	qthetaOffset         = 4  // QTHETA_OFFSET
	qthetaOffsetTwophase = 16 // QTHETA_OFFSET_TWOPHASE

	bitRes = entcode.BitRes
)

// AnalysisInfo is AnalysisInfo from celt.h. The encoder reads it.
type AnalysisInfo struct {
	Valid               int
	Tonality            float32
	TonalitySlope       float32
	Noisiness           float32
	Activity            float32
	MusicProb           float32
	MusicProbMin        float32
	MusicProbMax        float32
	Bandwidth           int
	ActivityProbability float32
	MaxPitchRatio       float32
	// LeakBoost is stored as Q6 to save space.
	LeakBoost [LeakBands]uint8
}

// SILKInfo is SILKInfo from celt.h.
type SILKInfo struct {
	SignalType int
	Offset     int
}

// bitsToBitrate is bits_to_bitrate.
func bitsToBitrate(bits, fs, frameSize int32) int32 {
	return bits * (6 * fs / frameSize) / 6
}

// bitrateToBits is bitrate_to_bits.
func bitrateToBits(bitrate, fs, frameSize int32) int32 {
	return bitrate * 6 / (6 * fs / frameSize)
}

// Entropy coding tables from celt.h.
var (
	trimIcdf   = []uint8{126, 124, 119, 109, 87, 41, 19, 9, 4, 2, 0}
	spreadIcdf = []uint8{25, 23, 2, 0}
	tapsetIcdf = []uint8{2, 1, 0}
)

// tfSelectTable is tf_select_table: the TF change table. Positive values
// mean better frequency resolution (longer effective window), negative
// values mean better time resolution (shorter effective window). The
// second index is 4*isTransient + 2*tf_select + per_band_flag.
var tfSelectTable = [4][8]int8{
	// isTransient=0     isTransient=1
	{0, -1, 0, -1, 0, -1, 0, -1}, // 2.5 ms
	{0, -1, 0, -2, 1, 0, 1, -1},  // 5 ms
	{0, -2, 0, -3, 2, 0, 1, -1},  // 10 ms
	{0, -2, 0, -3, 3, 0, 1, -1},  // 20 ms
}

// ResamplingFactor is resampling_factor. It returns 0 for an unsupported
// rate (the C code asserts).
func ResamplingFactor(rate int32) int {
	switch rate {
	case 48000:
		return 1
	case 24000:
		return 2
	case 16000:
		return 3
	case 12000:
		return 4
	case 8000:
		return 6
	}
	return 0
}

// combFilterConst is comb_filter_const_c (the non-ARM version). x is
// indexed from xo and reads back to x[xo-T-2].
func combFilterConst(y []float32, yo int, x []float32, xo int, T, N int, g10, g11, g12 float32) {
	x4 := x[xo-T-2]
	x3 := x[xo-T-1]
	x2 := x[xo-T]
	x1 := x[xo-T+1]
	for i := 0; i < N; i++ {
		x0 := x[xo+i-T+2]
		y[yo+i] = x[xo+i] + float32(g10*x2) + float32(g11*(x1+x3)) + float32(g12*(x0+x4))
		x4 = x3
		x3 = x2
		x2 = x1
		x1 = x0
	}
}

var combGains = [3][3]float32{
	{0.3066406250, 0.2170410156, 0.1296386719},
	{0.4638671875, 0.2680664062, 0},
	{0.7998046875, 0.1000976562, 0},
}

// combFilter is comb_filter. y[yo:] receives N samples; x[xo:] is the
// input, with at least max(T0, T1)+2 samples of history before xo. x and
// y may be the same buffer at the same offset (in-place filtering).
func combFilter(y []float32, yo int, x []float32, xo int, T0, T1, N int,
	g0, g1 float32, tapset0, tapset1 int, window []float32, overlap int) {
	inPlace := sameElem(y, yo, x, xo)
	if g0 == 0 && g1 == 0 {
		if !inPlace {
			copy(y[yo:yo+N], x[xo:xo+N])
		}
		return
	}
	// When the gain is zero, T0 and/or T1 is set to zero. We need to have
	// then be at least 2 to avoid processing garbage data.
	if T0 < CombFilterMinPeriod {
		T0 = CombFilterMinPeriod
	}
	if T1 < CombFilterMinPeriod {
		T1 = CombFilterMinPeriod
	}
	g00 := float32(g0 * combGains[tapset0][0])
	g01 := float32(g0 * combGains[tapset0][1])
	g02 := float32(g0 * combGains[tapset0][2])
	g10 := float32(g1 * combGains[tapset1][0])
	g11 := float32(g1 * combGains[tapset1][1])
	g12 := float32(g1 * combGains[tapset1][2])
	x1 := x[xo-T1+1]
	x2 := x[xo-T1]
	x3 := x[xo-T1-1]
	x4 := x[xo-T1-2]
	// If the filter didn't change, we don't need the overlap.
	if g0 == g1 && T0 == T1 && tapset0 == tapset1 {
		overlap = 0
	}
	i := 0
	for ; i < overlap; i++ {
		x0 := x[xo+i-T1+2]
		f := float32(window[i] * window[i])
		omf := 1 - f
		y[yo+i] = x[xo+i] +
			float32(float32(omf*g00)*x[xo+i-T0]) +
			float32(float32(omf*g01)*(x[xo+i-T0+1]+x[xo+i-T0-1])) +
			float32(float32(omf*g02)*(x[xo+i-T0+2]+x[xo+i-T0-2])) +
			float32(float32(f*g10)*x2) +
			float32(float32(f*g11)*(x1+x3)) +
			float32(float32(f*g12)*(x0+x4))
		x4 = x3
		x3 = x2
		x2 = x1
		x1 = x0
	}
	if g1 == 0 {
		if !inPlace {
			copy(y[yo+overlap:yo+N], x[xo+overlap:xo+N])
		}
		return
	}
	// Compute the part with the constant filter.
	combFilterConst(y, yo+i, x, xo+i, T1, N-i, g10, g11, g12)
}

// sameElem reports whether a[ao] and b[bo] are the same element (the C
// pointer comparison x != y).
func sameElem(a []float32, ao int, b []float32, bo int) bool {
	return ao < len(a) && bo < len(b) && &a[ao] == &b[bo]
}

// initCaps is init_caps.
func initCaps(m *Mode, cap []int, LM, C int) {
	for i := 0; i < m.NbEBands; i++ {
		N := int(m.EBands[i+1]-m.EBands[i]) << uint(LM)
		cap[i] = (int(m.Cache.Caps[m.NbEBands*(2*LM+C-1)+i]) + 64) * C * N >> 2
	}
}

// floorInt is (int)floor(x) for a float argument as compiled on x86-64:
// floor in double precision, then cvttsd2si, which gives INT32_MIN when
// the result does not fit in an int32 (including NaN).
func floorInt(x float32) int {
	return truncInt(math.Floor(float64(x)))
}

// truncInt is a C (int) conversion of a double on x86-64 (cvttsd2si).
func truncInt(f float64) int {
	if !(f >= -2147483648 && f < 2147483648) {
		return math.MinInt32
	}
	return int(int32(f))
}

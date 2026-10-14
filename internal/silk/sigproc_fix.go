// Ported from libopus 1.6.1 silk/SigProc_FIX.h and silk/typedef.h.

package silk

// Integer limits (typedef.h).
const (
	int64MAX = int64(0x7FFFFFFFFFFFFFFF)
	int64MIN = -int64MAX - 1
	int32MAX = int32(0x7FFFFFFF)
	int32MIN = int32(-0x80000000)
	int16MAX = 0x7FFF
	int16MIN = -0x8000
	int8MAX  = 0x7F
	int8MIN  = -0x80
	uint8MAX = 0xFF
)

// silkMaxOrderLPC is SILK_MAX_ORDER_LPC: the maximum order of the LPC
// analysis in schur() and k2a().
const silkMaxOrderLPC = 24

// The macros below port SigProc_FIX.h. The C code relies on 32-bit two's
// complement wraparound in several places (silk_ADD32, silk_MLA, ...);
// Go int32 arithmetic wraps the same way.

// ror32 is silk_ROR32: rotate a32 right by rot bits (left if rot < 0).
func ror32(a32 int32, rot int) int32 {
	x := uint32(a32)
	r := uint32(rot)
	m := uint32(-rot)
	if rot == 0 {
		return a32
	} else if rot < 0 {
		return int32((x << m) | (x >> (32 - m)))
	}
	return int32((x << (32 - r)) | (x >> r))
}

// mul is silk_MUL.
func mul(a32, b32 int32) int32 { return a32 * b32 }

// mla is silk_MLA: a32 + b32*c32.
func mla(a32, b32, c32 int32) int32 { return a32 + b32*c32 }

// smultt is silk_SMULTT.
func smultt(a32, b32 int32) int32 { return (a32 >> 16) * (b32 >> 16) }

// smlatt is silk_SMLATT.
func smlatt(a32, b32, c32 int32) int32 { return a32 + (b32>>16)*(c32>>16) }

// smlalbb is silk_SMLALBB.
func smlalbb(a64 int64, b16, c16 int16) int64 { return a64 + int64(int32(b16)*int32(c16)) }

// smull is silk_SMULL.
func smull(a32, b32 int32) int64 { return int64(a32) * int64(b32) }

// add32ovflw is silk_ADD32_ovflw.
func add32ovflw(a, b int32) int32 { return int32(uint32(a) + uint32(b)) }

// sub32ovflw is silk_SUB32_ovflw.
func sub32ovflw(a, b int32) int32 { return int32(uint32(a) - uint32(b)) }

// mlaOvflw is silk_MLA_ovflw.
func mlaOvflw(a32, b32, c32 int32) int32 { return add32ovflw(a32, int32(uint32(b32)*uint32(c32))) }

// smlabbOvflw is silk_SMLABB_ovflw.
func smlabbOvflw(a32, b32, c32 int32) int32 {
	return add32ovflw(a32, int32(int16(b32))*int32(int16(c32)))
}

// div32_16 is silk_DIV32_16.
func div32_16(a32, b16 int32) int32 { return a32 / b16 }

// div32 is silk_DIV32.
func div32(a32, b32 int32) int32 { return a32 / b32 }

// sat8 is silk_SAT8.
func sat8(a int32) int32 {
	if a > int8MAX {
		return int8MAX
	}
	if a < int8MIN {
		return int8MIN
	}
	return a
}

// sat16 is silk_SAT16.
func sat16(a int32) int32 {
	if a > int16MAX {
		return int16MAX
	}
	if a < int16MIN {
		return int16MIN
	}
	return a
}

// sat32 is silk_SAT32 for a 64-bit argument.
func sat32(a int64) int32 {
	if a > int64(int32MAX) {
		return int32MAX
	}
	if a < int64(int32MIN) {
		return int32MIN
	}
	return int32(a)
}

// addSat16 is silk_ADD_SAT16.
func addSat16(a, b int16) int16 { return int16(sat16(int32(a) + int32(b))) }

// subSat16 is silk_SUB_SAT16.
func subSat16(a, b int16) int16 { return int16(sat16(int32(a) - int32(b))) }

// addSat64 is silk_ADD_SAT64.
func addSat64(a, b int64) int64 {
	s := int64(uint64(a) + uint64(b))
	if uint64(s)&0x8000000000000000 == 0 {
		if uint64(a&b)&0x8000000000000000 != 0 {
			return int64MIN
		}
		return s
	}
	if uint64(a|b)&0x8000000000000000 == 0 {
		return int64MAX
	}
	return s
}

// subSat64 is silk_SUB_SAT64.
func subSat64(a, b int64) int64 {
	s := int64(uint64(a) - uint64(b))
	if uint64(s)&0x8000000000000000 == 0 {
		if uint64(a)&(uint64(b)^0x8000000000000000)&0x8000000000000000 != 0 {
			return int64MIN
		}
		return s
	}
	if (uint64(a)^0x8000000000000000)&uint64(b)&0x8000000000000000 != 0 {
		return int64MAX
	}
	return s
}

// posSat32 is silk_POS_SAT32 for a 64-bit argument.
func posSat32(a int64) int32 {
	if a > int64(int32MAX) {
		return int32MAX
	}
	return int32(a)
}

// addPosSat32 is silk_ADD_POS_SAT32.
func addPosSat32(a, b int32) int32 {
	if (uint32(a)+uint32(b))&0x80000000 != 0 {
		return int32MAX
	}
	return a + b
}

// lshift is silk_LSHIFT / silk_LSHIFT32 (shift >= 0, shift < 32).
func lshift(a int32, shift int) int32 { return int32(uint32(a) << uint(shift)) }

// lshift64 is silk_LSHIFT64.
func lshift64(a int64, shift int) int64 { return int64(uint64(a) << uint(shift)) }

// lshiftOvflw is silk_LSHIFT_ovflw.
func lshiftOvflw(a int32, shift int) int32 { return int32(uint32(a) << uint(shift)) }

// rshift is silk_RSHIFT / silk_RSHIFT32 (arithmetic shift).
func rshift(a int32, shift int) int32 { return a >> uint(shift) }

// rshift64 is silk_RSHIFT64.
func rshift64(a int64, shift int) int64 { return a >> uint(shift) }

// rshiftUint is silk_RSHIFT_uint.
func rshiftUint(a uint32, shift int) uint32 { return a >> uint(shift) }

// lshiftSat32 is silk_LSHIFT_SAT32: saturates before shifting.
func lshiftSat32(a int32, shift int) int32 {
	return lshift(limit32(a, int32MIN>>uint(shift), int32MAX>>uint(shift)), shift)
}

// addLshift32 is silk_ADD_LSHIFT / silk_ADD_LSHIFT32.
func addLshift32(a, b int32, shift int) int32 { return a + lshift(b, shift) }

// addRshift32 is silk_ADD_RSHIFT / silk_ADD_RSHIFT32.
func addRshift32(a, b int32, shift int) int32 { return a + (b >> uint(shift)) }

// subLshift32 is silk_SUB_LSHIFT32.
func subLshift32(a, b int32, shift int) int32 { return a - lshift(b, shift) }

// subRshift32 is silk_SUB_RSHIFT32.
func subRshift32(a, b int32, shift int) int32 { return a - (b >> uint(shift)) }

// rshiftRound is silk_RSHIFT_ROUND (shift > 0).
func rshiftRound(a int32, shift int) int32 {
	if shift == 1 {
		return (a >> 1) + (a & 1)
	}
	return ((a >> uint(shift-1)) + 1) >> 1
}

// rshiftRound64 is silk_RSHIFT_ROUND64 (shift > 0).
func rshiftRound64(a int64, shift int) int64 {
	if shift == 1 {
		return (a >> 1) + (a & 1)
	}
	return ((a >> uint(shift-1)) + 1) >> 1
}

// nshiftMul32_32 is silk_NSHIFT_MUL_32_32.
func nshiftMul32_32(a, b int32) int32 {
	return -(31 - (32 - clz32(abs32(a)) + (32 - clz32(abs32(b)))))
}

// nshiftMul16_16 is silk_NSHIFT_MUL_16_16.
func nshiftMul16_16(a, b int16) int32 {
	return -(15 - (16 - clz16(abs16(a)) + (16 - clz16(abs16(b)))))
}

// fixConst is SILK_FIX_CONST for constants evaluated at run time. Prefer
// precomputed constants where the C code uses SILK_FIX_CONST on literals.
func fixConst(c float64, q uint) int32 {
	return int32(float64(c*float64(int64(1)<<q)) + 0.5)
}

// fixConstF32 is SILK_FIX_CONST for a float (silk_float, "f" suffix)
// constant: C multiplies in float before adding 0.5 in double.
func fixConstF32(c float32, q uint) int32 {
	return int32(float64(c*float32(int64(1)<<q)) + 0.5)
}

// minInt is silk_min_int / silk_min for int.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// maxInt is silk_max_int / silk_max for int.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// min16 is silk_min_16.
func min16(a, b int16) int16 {
	if a < b {
		return a
	}
	return b
}

// max16 is silk_max_16.
func max16(a, b int16) int16 {
	if a > b {
		return a
	}
	return b
}

// min32 is silk_min_32 / silk_min for int32.
func min32(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

// max32 is silk_max_32 / silk_max for int32.
func max32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

// min64 is silk_min_64.
func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// max64 is silk_max_64.
func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// limit32 is silk_LIMIT / silk_LIMIT_32 for int32.
func limit32(a, limit1, limit2 int32) int32 {
	if limit1 > limit2 {
		if a > limit1 {
			return limit1
		}
		if a < limit2 {
			return limit2
		}
		return a
	}
	if a > limit2 {
		return limit2
	}
	if a < limit1 {
		return limit1
	}
	return a
}

// limitInt is silk_LIMIT / silk_LIMIT_int for int.
func limitInt(a, limit1, limit2 int) int {
	if limit1 > limit2 {
		if a > limit1 {
			return limit1
		}
		if a < limit2 {
			return limit2
		}
		return a
	}
	if a > limit2 {
		return limit2
	}
	if a < limit1 {
		return limit1
	}
	return a
}

// abs32 is silk_abs for int32 (wrong for int32MIN, as in C).
func abs32(a int32) int32 {
	if a > 0 {
		return a
	}
	return -a
}

// abs16 is silk_abs for int16 (wrong for int16MIN, as in C).
func abs16(a int16) int16 {
	if a > 0 {
		return a
	}
	return -a
}

// absInt is silk_abs for int.
func absInt(a int) int {
	if a > 0 {
		return a
	}
	return -a
}

// absInt32 is silk_abs_int32.
func absInt32(a int32) int32 { return (a ^ (a >> 31)) - (a >> 31) }

// abs64 is silk_abs_int64.
func abs64(a int64) int64 {
	if a > 0 {
		return a
	}
	return -a
}

// sign is silk_sign.
func sign(a int32) int32 {
	if a > 0 {
		return 1
	}
	if a < 0 {
		return -1
	}
	return 0
}

// Pseudo-random generator constants (RAND_MULTIPLIER, RAND_INCREMENT).
const (
	randMultiplier = 196314165
	randIncrement  = 907633515
)

// silkRAND is silk_RAND.
func silkRAND(seed int32) int32 { return mlaOvflw(randIncrement, seed, randMultiplier) }

// smmul is silk_SMMUL: signed top word multiply.
func smmul(a32, b32 int32) int32 { return int32(smull(a32, b32) >> 32) }

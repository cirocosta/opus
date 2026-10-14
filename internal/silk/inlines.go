// Ported from libopus 1.6.1 silk/Inlines.h.

package silk

// clz64 is silk_CLZ64.
func clz64(in int64) int32 {
	inUpper := int32(in >> 32)
	if inUpper == 0 {
		return 32 + clz32(int32(in))
	}
	return clz32(inUpper)
}

// clzFrac is silk_CLZ_FRAC: the number of leading zeros and the 7 bits
// right after the leading one.
func clzFrac(in int32) (lz, fracQ7 int32) {
	lzeros := clz32(in)
	return lzeros, ror32(in, int(24-lzeros)) & 0x7f
}

// sqrtApprox is silk_SQRT_APPROX.
func sqrtApprox(x int32) int32 {
	var y int32
	if x <= 0 {
		return 0
	}
	lz, fracQ7 := clzFrac(x)
	if lz&1 != 0 {
		y = 32768
	} else {
		y = 46214 // 46214 = sqrt(2) * 32768
	}
	// Get scaling right.
	y >>= uint(lz >> 1)
	// Increment using fractional part of input.
	y = smlawb(y, y, smulbb(213, fracQ7))
	return y
}

// div32VarQ is silk_DIV32_varQ: a good approximation of
// (a32 << Qres) / b32.
func div32VarQ(a32, b32 int32, qres int) int32 {
	// Compute number of bits head room and normalize inputs.
	aHeadrm := int(clz32(abs32(a32))) - 1
	a32Nrm := lshift(a32, aHeadrm)
	bHeadrm := int(clz32(abs32(b32))) - 1
	b32Nrm := lshift(b32, bHeadrm)

	// Inverse of b32, with 14 bits of precision.
	b32Inv := div32_16(int32MAX>>2, b32Nrm>>16)

	// First approximation.
	result := smulwb(a32Nrm, b32Inv)

	// Compute residual by subtracting product of denominator and first
	// approximation. It's OK to overflow because the final value of
	// a32Nrm should always be small.
	a32Nrm = sub32ovflw(a32Nrm, lshiftOvflw(smmul(b32Nrm, result), 3))

	// Refinement.
	result = smlawb(result, a32Nrm, b32Inv)

	// Convert to Qres domain.
	lsh := 29 + aHeadrm - bHeadrm - qres
	if lsh < 0 {
		return lshiftSat32(result, -lsh)
	}
	if lsh < 32 {
		return result >> uint(lsh)
	}
	// Avoid undefined result.
	return 0
}

// inverse32VarQ is silk_INVERSE32_varQ: a good approximation of
// (1 << Qres) / b32.
func inverse32VarQ(b32 int32, qres int) int32 {
	// Compute number of bits head room and normalize input.
	bHeadrm := int(clz32(abs32(b32))) - 1
	b32Nrm := lshift(b32, bHeadrm)

	// Inverse of b32, with 14 bits of precision.
	b32Inv := div32_16(int32MAX>>2, b32Nrm>>16)

	// First approximation.
	result := lshift(b32Inv, 16)

	// Compute residual by subtracting product of denominator and first
	// approximation from one.
	errQ32 := lshift((int32(1)<<29)-smulwb(b32Nrm, b32Inv), 3)

	// Refinement.
	result = smlaww(result, errQ32, b32Inv)

	// Convert to Qres domain.
	lsh := 61 - bHeadrm - qres
	if lsh <= 0 {
		return lshiftSat32(result, -lsh)
	}
	if lsh < 32 {
		return result >> uint(lsh)
	}
	// Avoid undefined result.
	return 0
}

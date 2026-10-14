// Ported from libopus 1.6.1 silk/macros.h.

package silk

import "math/bits"

// The reference build defines OPUS_FAST_INT64, so these use the 64-bit
// forms of the macros. Results are truncated to int32 with wraparound,
// as the C casts do.

// smulwb is silk_SMULWB: (a32 * (int16)b32) >> 16.
func smulwb(a32, b32 int32) int32 {
	return int32((int64(a32) * int64(int16(b32))) >> 16)
}

// smlawb is silk_SMLAWB: a32 + ((b32 * (int16)c32) >> 16).
func smlawb(a32, b32, c32 int32) int32 {
	return a32 + smulwb(b32, c32)
}

// smulwt is silk_SMULWT: (a32 * (b32 >> 16)) >> 16.
func smulwt(a32, b32 int32) int32 {
	return int32((int64(a32) * int64(b32>>16)) >> 16)
}

// smlawt is silk_SMLAWT: a32 + ((b32 * (c32 >> 16)) >> 16).
func smlawt(a32, b32, c32 int32) int32 {
	return a32 + smulwt(b32, c32)
}

// smulbb is silk_SMULBB: (int16)a32 * (int16)b32.
func smulbb(a32, b32 int32) int32 {
	return int32(int16(a32)) * int32(int16(b32))
}

// smlabb is silk_SMLABB: a32 + (int16)b32 * (int16)c32.
func smlabb(a32, b32, c32 int32) int32 {
	return a32 + int32(int16(b32))*int32(int16(c32))
}

// smulww is silk_SMULWW: (a32 * b32) >> 16.
func smulww(a32, b32 int32) int32 {
	return int32((int64(a32) * int64(b32)) >> 16)
}

// smlaww is silk_SMLAWW: a32 + ((b32 * c32) >> 16).
func smlaww(a32, b32, c32 int32) int32 {
	return a32 + smulww(b32, c32)
}

// Add/subtract with output saturated.

// addSat32 is silk_ADD_SAT32.
func addSat32(a, b int32) int32 {
	s := int32(uint32(a) + uint32(b))
	if uint32(s)&0x80000000 == 0 {
		if uint32(a&b)&0x80000000 != 0 {
			return int32MIN
		}
		return s
	}
	if uint32(a|b)&0x80000000 == 0 {
		return int32MAX
	}
	return s
}

// subSat32 is silk_SUB_SAT32.
func subSat32(a, b int32) int32 {
	s := int32(uint32(a) - uint32(b))
	if uint32(s)&0x80000000 == 0 {
		if uint32(a)&(uint32(b)^0x80000000)&0x80000000 != 0 {
			return int32MIN
		}
		return s
	}
	if (uint32(a)^0x80000000)&uint32(b)&0x80000000 != 0 {
		return int32MAX
	}
	return s
}

// clz32 is silk_CLZ32.
func clz32(in32 int32) int32 {
	return int32(bits.LeadingZeros32(uint32(in32)))
}

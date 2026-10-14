// Ported from libopus 1.6.1 celt/ecintrin.h.

package entcode

import "math/bits"

// Ilog returns the number of bits needed to represent v, that is one
// plus the index of the highest set bit, or 0 when v is 0. It ports
// EC_ILOG and ec_ilog.
func Ilog(v uint32) int {
	return bits.Len32(v)
}

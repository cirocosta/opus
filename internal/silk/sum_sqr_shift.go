// Ported from libopus 1.6.1 silk/sum_sqr_shift.c.

package silk

// sumSqrShift is silk_sum_sqr_shift: compute number of bits to right
// shift the sum of squares of a vector of int16s to make it fit in an
// int32.
func sumSqrShift(x []int16, length int) (energy int32, shift int) {
	var i int
	var nrgTmp uint32

	// Do a first run with the maximum shift we could have.
	shft := 31 - int(clz32(int32(length)))
	// Let's be conservative with rounding and start with nrg=len.
	nrg := int32(length)
	for i = 0; i < length-1; i += 2 {
		nrgTmp = uint32(smulbb(int32(x[i]), int32(x[i])))
		nrgTmp = uint32(smlabbOvflw(int32(nrgTmp), int32(x[i+1]), int32(x[i+1])))
		nrg = int32(uint32(nrg) + nrgTmp>>uint(shft))
	}
	if i < length {
		// One sample left to process.
		nrgTmp = uint32(smulbb(int32(x[i]), int32(x[i])))
		nrg = int32(uint32(nrg) + nrgTmp>>uint(shft))
	}
	// Make sure the result will fit in a 32-bit signed integer with two
	// bits of headroom.
	shft = int(max32(0, int32(shft)+3-clz32(nrg)))
	nrg = 0
	for i = 0; i < length-1; i += 2 {
		nrgTmp = uint32(smulbb(int32(x[i]), int32(x[i])))
		nrgTmp = uint32(smlabbOvflw(int32(nrgTmp), int32(x[i+1]), int32(x[i+1])))
		nrg = int32(uint32(nrg) + nrgTmp>>uint(shft))
	}
	if i < length {
		// One sample left to process.
		nrgTmp = uint32(smulbb(int32(x[i]), int32(x[i])))
		nrg = int32(uint32(nrg) + nrgTmp>>uint(shft))
	}

	return nrg, shft
}

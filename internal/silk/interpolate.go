// Ported from libopus 1.6.1 silk/interpolate.c.

package silk

// interpolate is silk_interpolate: interpolate two vectors. ifactQ2 is
// the interpolation factor, the weight on the second vector.
func interpolate(xi, x0, x1 []int16, ifactQ2, d int) {
	for i := 0; i < d; i++ {
		xi[i] = int16(
			addRshift32(int32(x0[i]), smulbb(int32(x1[i])-int32(x0[i]), int32(ifactQ2)), 2),
		)
	}
}

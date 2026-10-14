// Ported from libopus 1.6.1 silk/float/k2a_FLP.c.

package silk

// k2aFLP is silk_k2a_FLP: step up function, converts reflection
// coefficients to prediction coefficients.
func k2aFLP(a []float32, rc []float32, order int) {
	for k := 0; k < order; k++ {
		rck := rc[k]
		for n := 0; n < (k+1)>>1; n++ {
			tmp1 := a[n]
			tmp2 := a[k-n-1]
			a[n] = tmp1 + float32(tmp2*rck)
			a[k-n-1] = tmp2 + float32(tmp1*rck)
		}
		a[k] = -rck
	}
}

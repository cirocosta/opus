// Ported from libopus 1.6.1 silk/inner_prod_aligned.c.

package silk

// innerProdAlignedScale is silk_inner_prod_aligned_scale.
func innerProdAlignedScale(inVec1, inVec2 []int16, scale, length int) int32 {
	sum := int32(0)
	for i := 0; i < length; i++ {
		sum = addRshift32(sum, smulbb(int32(inVec1[i]), int32(inVec2[i])), scale)
	}
	return sum
}

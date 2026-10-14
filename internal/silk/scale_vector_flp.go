// Ported from libopus 1.6.1 silk/float/scale_vector_FLP.c.

package silk

// scaleVectorFLP is silk_scale_vector_FLP: multiply a vector by a
// constant.
func scaleVectorFLP(data1 []float32, gain float32, dataSize int) {
	for i := 0; i < dataSize; i++ {
		data1[i] = float32(data1[i] * gain)
	}
}

// Ported from libopus 1.6.1 silk/float/scale_copy_vector_FLP.c.

package silk

// scaleCopyVectorFLP is silk_scale_copy_vector_FLP: copy and multiply a
// vector by a constant.
func scaleCopyVectorFLP(dataOut []float32, dataIn []float32, gain float32, dataSize int) {
	for i := 0; i < dataSize; i++ {
		dataOut[i] = float32(gain * dataIn[i])
	}
}

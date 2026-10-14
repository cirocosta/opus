// Ported from libopus 1.6.1 silk/float/inner_product_FLP.c.

package silk

// innerProductFLP is silk_inner_product_FLP (silk_inner_product_FLP_c):
// inner product of two float arrays, with the result as double.
func innerProductFLP(data1, data2 []float32, dataSize int) float64 {
	// 4x unrolled loop.
	result := 0.0
	i := 0
	for ; i < dataSize-3; i += 4 {
		result += float64(float64(data1[i+0])*float64(data2[i+0])) +
			float64(float64(data1[i+1])*float64(data2[i+1])) +
			float64(float64(data1[i+2])*float64(data2[i+2])) +
			float64(float64(data1[i+3])*float64(data2[i+3]))
	}

	// Add any remaining products.
	for ; i < dataSize; i++ {
		result += float64(float64(data1[i]) * float64(data2[i]))
	}

	return result
}

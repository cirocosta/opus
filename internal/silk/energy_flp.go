// Ported from libopus 1.6.1 silk/float/energy_FLP.c.

package silk

// energyFLP is silk_energy_FLP: sum of squares of a float array, with
// the result as double.
func energyFLP(data []float32, dataSize int) float64 {
	// 4x unrolled loop.
	result := 0.0
	i := 0
	for ; i < dataSize-3; i += 4 {
		result += float64(float64(data[i+0])*float64(data[i+0])) +
			float64(float64(data[i+1])*float64(data[i+1])) +
			float64(float64(data[i+2])*float64(data[i+2])) +
			float64(float64(data[i+3])*float64(data[i+3]))
	}

	// Add any remaining products.
	for ; i < dataSize; i++ {
		result += float64(float64(data[i]) * float64(data[i]))
	}

	return result
}

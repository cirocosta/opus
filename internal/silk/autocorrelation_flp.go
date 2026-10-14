// Ported from libopus 1.6.1 silk/float/autocorrelation_FLP.c.

package silk

// autocorrelationFLP is silk_autocorrelation_FLP: compute
// autocorrelation.
func autocorrelationFLP(results []float32, inputData []float32, inputDataSize, correlationCount int) {
	if correlationCount > inputDataSize {
		correlationCount = inputDataSize
	}

	for i := 0; i < correlationCount; i++ {
		results[i] = float32(innerProductFLP(inputData, inputData[i:], inputDataSize-i))
	}
}

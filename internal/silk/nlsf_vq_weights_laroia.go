// Ported from libopus 1.6.1 silk/NLSF_VQ_weights_laroia.c.

package silk

// R. Laroia, N. Phamdo and N. Farvardin, "Robust and Efficient
// Quantization of Speech LSP Parameters Using Structured Vector
// Quantization", Proc. IEEE Int. Conf. Acoust., Speech, Signal
// Processing, pp. 641-644, 1991.

// nlsfVQWeightsLaroia is silk_NLSF_VQ_weights_laroia: Laroia low
// complexity NLSF weights.
func nlsfVQWeightsLaroia(pNLSFWQOut []int16, pNLSFQ15 []int16, d int) {
	// First value.
	tmp1Int := int32(maxInt(int(pNLSFQ15[0]), 1))
	tmp1Int = div32_16(int32(1)<<(15+nlsfWQ), tmp1Int)
	tmp2Int := int32(maxInt(int(pNLSFQ15[1])-int(pNLSFQ15[0]), 1))
	tmp2Int = div32_16(int32(1)<<(15+nlsfWQ), tmp2Int)
	pNLSFWQOut[0] = int16(minInt(int(tmp1Int+tmp2Int), int16MAX))

	// Main loop.
	for k := 1; k < d-1; k += 2 {
		tmp1Int = int32(maxInt(int(pNLSFQ15[k+1])-int(pNLSFQ15[k]), 1))
		tmp1Int = div32_16(int32(1)<<(15+nlsfWQ), tmp1Int)
		pNLSFWQOut[k] = int16(minInt(int(tmp1Int+tmp2Int), int16MAX))

		tmp2Int = int32(maxInt(int(pNLSFQ15[k+2])-int(pNLSFQ15[k+1]), 1))
		tmp2Int = div32_16(int32(1)<<(15+nlsfWQ), tmp2Int)
		pNLSFWQOut[k+1] = int16(minInt(int(tmp1Int+tmp2Int), int16MAX))
	}

	// Last value.
	tmp1Int = int32(maxInt((1<<15)-int(pNLSFQ15[d-1]), 1))
	tmp1Int = div32_16(int32(1)<<(15+nlsfWQ), tmp1Int)
	pNLSFWQOut[d-1] = int16(minInt(int(tmp1Int+tmp2Int), int16MAX))
}

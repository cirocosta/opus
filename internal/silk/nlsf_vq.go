// Ported from libopus 1.6.1 silk/NLSF_VQ.c.

package silk

// nlsfVQ is silk_NLSF_VQ: compute quantization errors for an LPC_order
// element input vector for a VQ codebook.
func nlsfVQ(errQ24 []int32, inQ15 []int16, pCBQ8 []uint8, pWghtQ9 []int16, k, lpcOrder int) {
	// Loop over codebook.
	cb := 0
	for i := 0; i < k; i++ {
		sumErrorQ24 := int32(0)
		predQ24 := int32(0)
		for m := lpcOrder - 2; m >= 0; m -= 2 {
			// Compute weighted absolute predictive quantization error for
			// index m + 1.
			diffQ15 := subLshift32(
				int32(inQ15[m+1]),
				int32(pCBQ8[cb+m+1]),
				7,
			) // range: [ -32767 : 32767 ]
			diffwQ24 := smulbb(diffQ15, int32(pWghtQ9[cb+m+1]))
			sumErrorQ24 = sumErrorQ24 + abs32(subRshift32(diffwQ24, predQ24, 1))
			predQ24 = diffwQ24

			// Compute weighted absolute predictive quantization error for
			// index m.
			diffQ15 = subLshift32(
				int32(inQ15[m]),
				int32(pCBQ8[cb+m]),
				7,
			) // range: [ -32767 : 32767 ]
			diffwQ24 = smulbb(diffQ15, int32(pWghtQ9[cb+m]))
			sumErrorQ24 = sumErrorQ24 + abs32(subRshift32(diffwQ24, predQ24, 1))
			predQ24 = diffwQ24
		}
		errQ24[i] = sumErrorQ24
		cb += lpcOrder
	}
}

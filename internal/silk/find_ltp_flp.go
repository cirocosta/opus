// Ported from libopus 1.6.1 silk/float/find_LTP_FLP.c.

package silk

// findLTPFLP is silk_find_LTP_FLP. r[rOff] is the C r_ptr: the LPC
// residual of the frame, with the pitch history before it.
func findLTPFLP(
	xx *[maxNbSubfr * ltpOrder * ltpOrder]float32,
	xX *[maxNbSubfr * ltpOrder]float32,
	r []float32,
	rOff int,
	lag *[maxNbSubfr]int,
	subfrLength, nbSubfr int,
) {
	xXPtr := 0
	xxPtr := 0
	rPtr := rOff
	for k := 0; k < nbSubfr; k++ {
		lagPtr := rPtr - (lag[k] + ltpOrder/2)
		corrMatrixFLP(r[lagPtr:], subfrLength, ltpOrder, xx[xxPtr:])
		corrVectorFLP(r[lagPtr:], r[rPtr:], subfrLength, ltpOrder, xX[xXPtr:])
		xxv := float32(energyFLP(r[rPtr:], subfrLength+ltpOrder))
		temp := 1.0 / maxFloat(
			xxv,
			float32(float32(ltpCorrInvMax*0.5)*(xx[xxPtr+0]+xx[xxPtr+24]))+1.0,
		)
		scaleVectorFLP(xx[xxPtr:], temp, ltpOrder*ltpOrder)
		scaleVectorFLP(xX[xXPtr:], temp, ltpOrder)

		rPtr += subfrLength
		xxPtr += ltpOrder * ltpOrder
		xXPtr += ltpOrder
	}
}

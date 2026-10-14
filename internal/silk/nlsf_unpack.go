// Ported from libopus 1.6.1 silk/NLSF_unpack.c.

package silk

// nlsfUnpack is silk_NLSF_unpack: unpack predictor values and indices
// for entropy coding tables.
func nlsfUnpack(ecIx []int16, predQ8 []uint8, psNLSFCB *nlsfCBStruct, cb1Index int) {
	order := int(psNLSFCB.order)
	ecSelPtr := psNLSFCB.ecSel[cb1Index*order/2:]
	for i := 0; i < order; i += 2 {
		entry := ecSelPtr[0]
		ecSelPtr = ecSelPtr[1:]
		ecIx[i] = int16(smulbb(int32(entry>>1)&7, 2*nlsfQuantMaxAmplitude+1))
		predQ8[i] = psNLSFCB.predQ8[i+int(entry&1)*(order-1)]
		ecIx[i+1] = int16(smulbb(int32(entry>>5)&7, 2*nlsfQuantMaxAmplitude+1))
		predQ8[i+1] = psNLSFCB.predQ8[i+int((entry>>4)&1)*(order-1)+1]
	}
}

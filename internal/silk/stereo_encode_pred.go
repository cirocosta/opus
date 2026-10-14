// Ported from libopus 1.6.1 silk/stereo_encode_pred.c.

package silk

import "github.com/cirocosta/opus/internal/entcode"

// stereoEncodePred is silk_stereo_encode_pred: entropy code the mid/side
// quantization indices.
func stereoEncodePred(psRangeEnc *entcode.Encoder, ix *[2][3]int8) {
	// Entropy coding.
	n := 5*int(ix[0][2]) + int(ix[1][2])
	psRangeEnc.EncIcdf(n, stereoPredJointICDF[:], 8)
	for n = 0; n < 2; n++ {
		psRangeEnc.EncIcdf(int(ix[n][0]), uniform3ICDF[:], 8)
		psRangeEnc.EncIcdf(int(ix[n][1]), uniform5ICDF[:], 8)
	}
}

// stereoEncodeMidOnly is silk_stereo_encode_mid_only: entropy code the
// mid-only flag.
func stereoEncodeMidOnly(psRangeEnc *entcode.Encoder, midOnlyFlag int8) {
	// Encode flag that only mid channel is coded.
	psRangeEnc.EncIcdf(int(midOnlyFlag), stereoOnlyCodeMidICDF[:], 8)
}

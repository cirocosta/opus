// Ported from libopus 1.6.1 silk/stereo_decode_pred.c.

package silk

import "github.com/cirocosta/opus/internal/entcode"

// stereoDecodePred is silk_stereo_decode_pred: decode mid/side
// predictors.
func stereoDecodePred(psRangeDec *entcode.Decoder, predQ13 *[2]int32) {
	var ix [2][3]int

	// Entropy decoding.
	n := psRangeDec.DecIcdf(stereoPredJointICDF[:], 8)
	ix[0][2] = n / 5
	ix[1][2] = n - 5*ix[0][2]
	for n = 0; n < 2; n++ {
		ix[n][0] = psRangeDec.DecIcdf(uniform3ICDF[:], 8)
		ix[n][1] = psRangeDec.DecIcdf(uniform5ICDF[:], 8)
	}

	// Dequantize.
	for n = 0; n < 2; n++ {
		ix[n][0] += 3 * ix[n][2]
		lowQ13 := int32(stereoPredQuantQ13[ix[n][0]])
		stepQ13 := smulwb(
			int32(stereoPredQuantQ13[ix[n][0]+1])-lowQ13,
			6554,
		) // SILK_FIX_CONST( 0.5 / STEREO_QUANT_SUB_STEPS, 16 )
		predQ13[n] = smlabb(lowQ13, stepQ13, int32(2*ix[n][1]+1))
	}

	// Subtract second from first predictor (helps when actually applying
	// these).
	predQ13[0] -= predQ13[1]
}

// stereoDecodeMidOnly is silk_stereo_decode_mid_only: decode the flag
// that only the mid channel is coded.
func stereoDecodeMidOnly(psRangeDec *entcode.Decoder) int {
	return psRangeDec.DecIcdf(stereoOnlyCodeMidICDF[:], 8)
}

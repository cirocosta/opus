// Ported from libopus 1.6.1 silk/resampler_private_AR2.c.

package silk

// resamplerPrivateAR2 is silk_resampler_private_AR2: second order AR
// filter with single delay elements. s is the state vector [2].
func resamplerPrivateAR2(s []int32, outQ8 []int32, in []int16, aQ14 []int16, len int) {
	for k := 0; k < len; k++ {
		out32 := addLshift32(s[0], int32(in[k]), 8)
		outQ8[k] = out32
		out32 = lshift(out32, 2)
		s[0] = smlawb(s[1], out32, int32(aQ14[0]))
		s[1] = smulwb(out32, int32(aQ14[1]))
	}
}

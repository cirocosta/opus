package kernels

// WarpedFeedback updates SILK's allpass state and returns its Q11 feedback.
// coeff contains an even number of Q13 taps, at least two; state has the same
// length. diff is Q14, and the low 16 bits of warp represent a signed Q16 value.
func WarpedFeedback(state []int32, coeff []int16, diff, warp int32) int32 {
	state = state[:len(coeff)]
	return warpedFeedback(state, coeff, diff, warp)
}

func feedbackProduct(a int32, b int16) int32 {
	return int32(int64(a) * int64(b) >> 16)
}

func warpedFeedbackGeneric(state []int32, coeff []int16, diff, warp int32) int32 {
	order := len(coeff)
	w := int16(warp)
	tmp2 := diff + feedbackProduct(state[0], w)
	tmp1 := state[0] + feedbackProduct(state[1]-tmp2, w)
	state[0] = tmp2
	out := int32(order>>1) + feedbackProduct(tmp2, coeff[0])
	for j := 2; j < order; j += 2 {
		tmp2 = state[j-1] + feedbackProduct(state[j]-tmp1, w)
		state[j-1] = tmp1
		out += feedbackProduct(tmp1, coeff[j-1])
		tmp1 = state[j] + feedbackProduct(state[j+1]-tmp2, w)
		state[j] = tmp2
		out += feedbackProduct(tmp2, coeff[j])
	}
	state[order-1] = tmp1
	return out + feedbackProduct(tmp1, coeff[order-1])
}

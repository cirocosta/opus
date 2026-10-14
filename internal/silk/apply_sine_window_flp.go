// Ported from libopus 1.6.1 silk/float/apply_sine_window_FLP.c.

package silk

// applySineWindowFLP is silk_apply_sine_window_FLP: apply a sine window
// to a signal vector. Window types: 1 -> sine window from 0 to pi/2;
// 2 -> sine window from pi/2 to pi. length must be a multiple of 4.
func applySineWindowFLP(pxWin []float32, px []float32, winType, length int) {
	var s0, s1 float32

	freq := piFLP / float32(length+1)

	// Approximation of 2 * cos(f).
	c := 2.0 - float32(freq*freq)

	// Initialize state.
	if winType < 2 {
		// Start from 0.
		s0 = 0.0
		// Approximation of sin(f).
		s1 = freq
	} else {
		// Start from 1.
		s0 = 1.0
		// Approximation of cos(f).
		s1 = float32(0.5 * c)
	}

	// Uses the recursive equation: sin(n*f) = 2 * cos(f) * sin((n-1)*f) -
	// sin((n-2)*f). 4 samples at a time.
	for k := 0; k < length; k += 4 {
		pxWin[k+0] = float32(float32(px[k+0]*0.5) * (s0 + s1))
		pxWin[k+1] = float32(px[k+1] * s1)
		s0 = float32(c*s1) - s0
		pxWin[k+2] = float32(float32(px[k+2]*0.5) * (s1 + s0))
		pxWin[k+3] = float32(px[k+3] * s0)
		s1 = float32(c*s0) - s1
	}
}

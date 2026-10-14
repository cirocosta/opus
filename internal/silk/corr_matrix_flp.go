// Ported from libopus 1.6.1 silk/float/corrMatrix_FLP.c.

package silk

// Correlation matrix computations for LS estimate.

// corrVectorFLP is silk_corrVector_FLP: calculates the correlation
// vector X'*t. x is the x vector [L+order-1] used to create X, t the
// target vector [L].
func corrVectorFLP(x []float32, t []float32, l, order int, xt []float32) {
	ptr1 := order - 1 // Points to first sample of column 0 of X: X[:,0]
	for lag := 0; lag < order; lag++ {
		// Calculate X[:,lag]'*t.
		xt[lag] = float32(innerProductFLP(x[ptr1:], t, l))
		ptr1-- // Next column of X
	}
}

// corrMatrixFLP is silk_corrMatrix_FLP: calculates the correlation
// matrix X'*X. x is the x vector [L+order-1] used to create X.
func corrMatrixFLP(x []float32, l, order int, xx []float32) {
	p1 := order - 1                // First sample of column 0 of X
	energy := energyFLP(x[p1:], l) // X[:,0]'*X[:,0]
	xx[0] = float32(energy)
	for j := 1; j < order; j++ {
		// Calculate X[:,j]'*X[:,j].
		energy += float64(float32(x[p1-j]*x[p1-j]) - float32(x[p1+l-j]*x[p1+l-j]))
		xx[j*order+j] = float32(energy)
	}

	p2 := order - 2 // First sample of column 1 of X
	for lag := 1; lag < order; lag++ {
		// Calculate X[:,0]'*X[:,lag].
		energy = innerProductFLP(x[p1:], x[p2:], l)
		xx[lag*order+0] = float32(energy)
		xx[0*order+lag] = float32(energy)
		// Calculate X[:,j]'*X[:,j + lag].
		for j := 1; j < order-lag; j++ {
			energy += float64(float32(x[p1-j]*x[p2-j]) - float32(x[p1+l-j]*x[p2+l-j]))
			xx[(lag+j)*order+j] = float32(energy)
			xx[j*order+lag+j] = float32(energy)
		}
		p2-- // Next column of X
	}
}

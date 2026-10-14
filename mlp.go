// Ported from libopus 1.6.1 src/mlp.h and src/mlp.c.

package opus

import (
	"github.com/cirocosta/opus/internal/dsp"
	"github.com/cirocosta/opus/internal/kernels"
)

const (
	// weightsScale is WEIGHTS_SCALE.
	weightsScale = float32(1.0 / 128)
	// maxNeurons is MAX_NEURONS.
	maxNeurons = 32
)

// analysisDenseLayer is AnalysisDenseLayer.
type analysisDenseLayer struct {
	bias         []int8
	inputWeights []int8
	nbInputs     int
	nbNeurons    int
	sigmoid      bool
}

// analysisGRULayer is AnalysisGRULayer.
type analysisGRULayer struct {
	bias             []int8
	inputWeights     []int8
	recurrentWeights []int8
	nbInputs         int
	nbNeurons        int
}

// tansigApprox is tansig_approx.
func tansigApprox(x float32) float32 {
	const (
		n0 = float32(952.52801514)
		n1 = float32(96.39235687)
		n2 = float32(0.60863042)
		d0 = float32(952.72399902)
		d1 = float32(413.36801147)
		d2 = float32(11.88600922)
	)
	x2 := float32(x * x)
	num := float32(float32(float32(float32(n2*x2)+n1)*x2) + n0)
	den := float32(float32(float32(float32(d2*x2)+d1)*x2) + d0)
	num = float32(num*x) / den
	return dsp.Max32(-1, dsp.Min32(1, num))
}

// sigmoidApprox is sigmoid_approx.
func sigmoidApprox(x float32) float32 {
	return .5 + float32(.5*tansigApprox(float32(.5*x)))
}

// gemmAccum is gemm_accum.
func gemmAccum(out []float32, weights []int8, rows, cols, colStride int, x []float32) {
	kernels.DenseAccum(out, weights, x, rows, cols, colStride)
}

// analysisComputeDense is analysis_compute_dense.
func analysisComputeDense(layer *analysisDenseLayer, output, input []float32) {
	m := layer.nbInputs
	n := layer.nbNeurons
	stride := n
	for i := 0; i < n; i++ {
		output[i] = float32(layer.bias[i])
	}
	gemmAccum(output, layer.inputWeights, n, m, stride, input)
	for i := 0; i < n; i++ {
		output[i] = float32(output[i] * weightsScale)
	}
	if layer.sigmoid {
		for i := 0; i < n; i++ {
			output[i] = sigmoidApprox(output[i])
		}
	} else {
		for i := 0; i < n; i++ {
			output[i] = tansigApprox(output[i])
		}
	}
}

// analysisComputeGRU is analysis_compute_gru.
func analysisComputeGRU(gru *analysisGRULayer, state, input []float32) {
	var tmp, z, r, h [maxNeurons]float32
	m := gru.nbInputs
	n := gru.nbNeurons
	stride := 3 * n
	// Compute update gate.
	for i := 0; i < n; i++ {
		z[i] = float32(gru.bias[i])
	}
	gemmAccum(z[:], gru.inputWeights, n, m, stride, input)
	gemmAccum(z[:], gru.recurrentWeights, n, n, stride, state)
	for i := 0; i < n; i++ {
		z[i] = sigmoidApprox(float32(weightsScale * z[i]))
	}

	// Compute reset gate.
	for i := 0; i < n; i++ {
		r[i] = float32(gru.bias[n+i])
	}
	gemmAccum(r[:], gru.inputWeights[n:], n, m, stride, input)
	gemmAccum(r[:], gru.recurrentWeights[n:], n, n, stride, state)
	for i := 0; i < n; i++ {
		r[i] = sigmoidApprox(float32(weightsScale * r[i]))
	}

	// Compute output.
	for i := 0; i < n; i++ {
		h[i] = float32(gru.bias[2*n+i])
	}
	for i := 0; i < n; i++ {
		tmp[i] = float32(state[i] * r[i])
	}
	gemmAccum(h[:], gru.inputWeights[2*n:], n, m, stride, input)
	gemmAccum(h[:], gru.recurrentWeights[2*n:], n, n, stride, tmp[:])
	for i := 0; i < n; i++ {
		h[i] = float32(z[i]*state[i]) + float32((1-z[i])*tansigApprox(float32(weightsScale*h[i])))
	}
	for i := 0; i < n; i++ {
		state[i] = h[i]
	}
}

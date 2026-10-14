package kernels

// DenseAccum adds a column-major signed-byte matrix multiplied by x to out.
// stride is the distance between columns, in elements. Each output accumulates
// columns in increasing order. out must not overlap x.
func DenseAccum(out []float32, weights []int8, x []float32, rows, cols, stride int) {
	out = out[:rows]
	x = x[:cols]
	if rows == 0 || cols == 0 {
		return
	}
	weights = weights[:(cols-1)*stride+rows]
	denseAccum(out, weights, x, rows, cols, stride)
}

func denseAccumGeneric(out []float32, weights []int8, x []float32, rows, cols, stride int) {
	i := 0
	for ; i+4 <= rows; i += 4 {
		s0, s1, s2, s3 := out[i], out[i+1], out[i+2], out[i+3]
		for j, v := range x[:cols] {
			w := weights[j*stride+i : j*stride+i+4]
			s0 += float32(float32(w[0]) * v)
			s1 += float32(float32(w[1]) * v)
			s2 += float32(float32(w[2]) * v)
			s3 += float32(float32(w[3]) * v)
		}
		out[i], out[i+1], out[i+2], out[i+3] = s0, s1, s2, s3
	}
	for ; i < rows; i++ {
		sum := out[i]
		for j, v := range x[:cols] {
			sum += float32(float32(weights[j*stride+i]) * v)
		}
		out[i] = sum
	}
}

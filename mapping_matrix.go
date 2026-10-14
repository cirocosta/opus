// Ported from libopus 1.6.1 src/mapping_matrix.c and src/mapping_matrix.h
// (floating-point build, where opus_res is float).

package opus

// mappingMatrix ports MappingMatrix. In C the cell data follows the header
// in memory; here it is the data slice. Cells are in column-major order.
type mappingMatrix struct {
	rows int // number of channels output from the matrix
	cols int // number of channels input to the matrix
	gain int // in dB, S7.8 format
	data []int16
}

// matrixIndex ports MATRIX_INDEX.
func matrixIndex(nbRows, row, col int) int { return nbRows*col + row }

// opusAlign ports align() from opus_private.h for a 64-bit target, where
// the union of a pointer, opus_int32, and opus_val32 aligns to 8 bytes.
func opusAlign(i int32) int32 {
	const alignment = 8
	return ((i + alignment - 1) / alignment) * alignment
}

// mappingMatrixGetSize ports mapping_matrix_get_size. It returns the size
// in bytes that the C struct uses, or 0 if the matrix is too large. The C
// header (three ints) is 12 bytes.
func mappingMatrixGetSize(rows, cols int) int32 {
	// Mapping Matrix must only support up to 255 channels in or out.
	// Additionally, the total cell count must be <= 65004 octets in order
	// for the matrix to be stored in an OGG header.
	if rows > 255 || cols > 255 {
		return 0
	}
	size := int32(rows) * int32(cols) * 2
	if size > 65004 {
		return 0
	}
	return opusAlign(12) + opusAlign(size)
}

// mappingMatrixGetData ports mapping_matrix_get_data.
func mappingMatrixGetData(matrix *mappingMatrix) []int16 {
	return matrix.data
}

// mappingMatrixInit ports mapping_matrix_init. It copies rows*cols cells
// from data into storage owned by matrix, reusing it when large enough.
func mappingMatrixInit(matrix *mappingMatrix, rows, cols, gain int, data []int16) {
	matrix.rows = rows
	matrix.cols = cols
	matrix.gain = gain
	n := rows * cols
	if cap(matrix.data) < n {
		matrix.data = make([]int16, n)
	}
	matrix.data = matrix.data[:n]
	copy(matrix.data, data[:n])
}

// mappingMatrixMultiplyChannelInFloat ports
// mapping_matrix_multiply_channel_in_float. output starts at the first
// sample of the output row; samples are output_rows apart.
func mappingMatrixMultiplyChannelInFloat(matrix *mappingMatrix, input []float32,
	inputRows int, output []float32, outputRow, outputRows, frameSize int) {
	// Matrix data is ordered col-wise.
	matrixData := mappingMatrixGetData(matrix)
	for i := 0; i < frameSize; i++ {
		var tmp float32
		for col := 0; col < inputRows; col++ {
			tmp += float32(float32(matrixData[matrixIndex(matrix.rows, outputRow, col)]) *
				input[matrixIndex(inputRows, col, i)])
		}
		output[outputRows*i] = (1 / 32768.0) * tmp
	}
}

// mappingMatrixMultiplyChannelOutFloat ports
// mapping_matrix_multiply_channel_out_float. input starts at the first
// sample of the input row; samples are input_rows apart.
func mappingMatrixMultiplyChannelOutFloat(matrix *mappingMatrix, input []float32,
	inputRow, inputRows int, output []float32, outputRows, frameSize int) {
	// Matrix data is ordered col-wise.
	matrixData := mappingMatrixGetData(matrix)
	for i := 0; i < frameSize; i++ {
		inputSample := input[inputRows*i]
		for row := 0; row < outputRows; row++ {
			tmp := float32(float32((1/32768.0)*
				float32(matrixData[matrixIndex(matrix.rows, row, inputRow)])) *
				inputSample)
			output[matrixIndex(outputRows, row, i)] += tmp
		}
	}
}

// mappingMatrixMultiplyChannelInShort ports
// mapping_matrix_multiply_channel_in_short.
func mappingMatrixMultiplyChannelInShort(matrix *mappingMatrix, input []int16,
	inputRows int, output []float32, outputRow, outputRows, frameSize int) {
	// Matrix data is ordered col-wise.
	matrixData := mappingMatrixGetData(matrix)
	for i := 0; i < frameSize; i++ {
		var tmp float32
		for col := 0; col < inputRows; col++ {
			tmp += float32(int32(matrixData[matrixIndex(matrix.rows, outputRow, col)]) *
				int32(input[matrixIndex(inputRows, col, i)]))
		}
		output[outputRows*i] = (1 / (32768.0 * 32768.0)) * tmp
	}
}

// mappingMatrixMultiplyChannelOutShort ports
// mapping_matrix_multiply_channel_out_short.
func mappingMatrixMultiplyChannelOutShort(matrix *mappingMatrix, input []float32,
	inputRow, inputRows int, output []int16, outputRows, frameSize int) {
	// Matrix data is ordered col-wise.
	matrixData := mappingMatrixGetData(matrix)
	for i := 0; i < frameSize; i++ {
		inputSample := int32(res2int16(input[inputRows*i]))
		for row := 0; row < outputRows; row++ {
			tmp := int32(matrixData[matrixIndex(matrix.rows, row, inputRow)]) * inputSample
			output[matrixIndex(outputRows, row, i)] += int16((tmp + 16384) >> 15)
		}
	}
}

// mappingMatrixMultiplyChannelInInt24 ports
// mapping_matrix_multiply_channel_in_int24. In the float build opus_val64
// is float.
func mappingMatrixMultiplyChannelInInt24(matrix *mappingMatrix, input []int32,
	inputRows int, output []float32, outputRow, outputRows, frameSize int) {
	// Matrix data is ordered col-wise.
	matrixData := mappingMatrixGetData(matrix)
	for i := 0; i < frameSize; i++ {
		var tmp float32
		for col := 0; col < inputRows; col++ {
			tmp += float32(float32(matrixData[matrixIndex(matrix.rows, outputRow, col)]) *
				float32(input[matrixIndex(inputRows, col, i)]))
		}
		output[outputRows*i] = int24ToRes((1 / 32768.0) * tmp)
	}
}

// mappingMatrixMultiplyChannelOutInt24 ports
// mapping_matrix_multiply_channel_out_int24.
func mappingMatrixMultiplyChannelOutInt24(matrix *mappingMatrix, input []float32,
	inputRow, inputRows int, output []int32, outputRows, frameSize int) {
	// Matrix data is ordered col-wise.
	matrixData := mappingMatrixGetData(matrix)
	for i := 0; i < frameSize; i++ {
		inputSample := res2int24(input[inputRows*i])
		for row := 0; row < outputRows; row++ {
			tmp := int64(matrixData[matrixIndex(matrix.rows, row, inputRow)]) * int64(inputSample)
			idx := matrixIndex(outputRows, row, i)
			output[idx] = int32(int64(output[idx]) + (tmp+16384)>>15)
		}
	}
}

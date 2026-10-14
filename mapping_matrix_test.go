package opus

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
)

func init() {
	registerGolden("matrix", genMatrixInputs, runMatrix)
}

type staticMatrix struct {
	m    *mappingMatrix
	data []int16
}

// staticMatrices lists the static matrices in the order of the C driver.
var staticMatrices = []staticMatrix{
	{&mappingMatrixFoaMixing, mappingMatrixFoaMixingData[:]},
	{&mappingMatrixSoaMixing, mappingMatrixSoaMixingData[:]},
	{&mappingMatrixToaMixing, mappingMatrixToaMixingData[:]},
	{&mappingMatrixFourthoaMixing, mappingMatrixFourthoaMixingData[:]},
	{&mappingMatrixFifthoaMixing, mappingMatrixFifthoaMixingData[:]},
	{&mappingMatrixFoaDemixing, mappingMatrixFoaDemixingData[:]},
	{&mappingMatrixSoaDemixing, mappingMatrixSoaDemixingData[:]},
	{&mappingMatrixToaDemixing, mappingMatrixToaDemixingData[:]},
	{&mappingMatrixFourthoaDemixing, mappingMatrixFourthoaDemixingData[:]},
	{&mappingMatrixFifthoaDemixing, mappingMatrixFifthoaDemixingData[:]},
}

// Matrix records: u8 kind. Kind 0 reports the static tables and
// mapping_matrix_get_size. Kind 1: u8 matrix index, u16 frame size, u8
// input rows, u8 output rows, then input rows * frame size float32
// samples, as many int16 samples, as many int32 samples, and as many
// float32 samples for the "out" functions.
func genMatrixInputs(r *rand.Rand) [][]byte {
	recs := [][]byte{{0}}
	for range 120 {
		var w recWriter
		w.u8(1)
		idx := r.IntN(len(staticMatrices))
		m := staticMatrices[idx].m
		fs := 1 + r.IntN(120)
		in := 1 + r.IntN(m.cols)
		out := 1 + r.IntN(m.rows)
		if r.IntN(3) == 0 {
			in, out = m.cols, m.rows
		}
		w.u8(idx)
		w.u16(fs)
		w.u8(in)
		w.u8(out)
		n := in * fs
		for range n {
			w.b = binary.LittleEndian.AppendUint32(w.b, math.Float32bits(float32(int32(r.Uint32())>>8)/(1<<23)))
		}
		for range n {
			w.u16(int(int16(r.Uint32())))
		}
		for range n {
			v := int32(r.Uint32()) >> 8
			if r.IntN(50) == 0 {
				v = int32(r.Uint32())
			}
			w.b = binary.LittleEndian.AppendUint32(w.b, uint32(v))
		}
		for range n {
			w.b = binary.LittleEndian.AppendUint32(w.b, math.Float32bits(float32(r.IntN(2516582)-1258291)/(1<<20)))
		}
		recs = append(recs, w.b)
	}
	return recs
}

func hashFloats(x []float32) uint64 {
	var b []byte
	for _, v := range x {
		b = binary.LittleEndian.AppendUint32(b, math.Float32bits(v))
	}
	return fnvHash(b)
}

func runMatrix(rec []byte) string {
	rd := &recReader{b: rec}
	if rd.u8() == 0 {
		var parts []string
		for _, s := range staticMatrices {
			var b []byte
			for _, v := range s.data {
				b = binary.LittleEndian.AppendUint16(b, uint16(v))
			}
			parts = append(parts, fmt.Sprintf("%d.%d.%d.%d:%016x", s.m.rows, s.m.cols,
				s.m.gain, len(s.data), fnvHash(b)))
		}
		for _, rc := range [][2]int{{1, 1}, {6, 6}, {38, 38}, {255, 127}, {255, 128}, {256, 1}, {3, 5}} {
			parts = append(parts, fmt.Sprintf("sz%d", mappingMatrixGetSize(rc[0], rc[1])))
		}
		return strings.Join(parts, " ")
	}
	s := staticMatrices[rd.u8()]
	fs, in, out := rd.u16(), rd.u8(), rd.u8()
	n := in * fs
	f32 := func() []float32 {
		x := make([]float32, n)
		for i := range x {
			x[i] = math.Float32frombits(binary.LittleEndian.Uint32(rd.bytes(4)))
		}
		return x
	}
	inFloat := f32()
	inShort := make([]int16, n)
	for i := range inShort {
		inShort[i] = int16(rd.u16())
	}
	inInt24 := make([]int32, n)
	for i := range inInt24 {
		inInt24[i] = int32(binary.LittleEndian.Uint32(rd.bytes(4)))
	}
	res := f32()

	var m mappingMatrix
	mappingMatrixInit(&m, s.m.rows, s.m.cols, s.m.gain, s.data)

	var parts []string
	o := make([]float32, out*fs)
	for row := 0; row < out; row++ {
		mappingMatrixMultiplyChannelInFloat(&m, inFloat, in, o[row:], row, out, fs)
	}
	parts = append(parts, fmt.Sprintf("if%016x", hashFloats(o)))
	clear(o)
	for row := 0; row < out; row++ {
		mappingMatrixMultiplyChannelInShort(&m, inShort, in, o[row:], row, out, fs)
	}
	parts = append(parts, fmt.Sprintf("is%016x", hashFloats(o)))
	clear(o)
	for row := 0; row < out; row++ {
		mappingMatrixMultiplyChannelInInt24(&m, inInt24, in, o[row:], row, out, fs)
	}
	parts = append(parts, fmt.Sprintf("ii%016x", hashFloats(o)))

	clear(o)
	for row := 0; row < in; row++ {
		mappingMatrixMultiplyChannelOutFloat(&m, res[row:], row, in, o, out, fs)
	}
	parts = append(parts, fmt.Sprintf("of%016x", hashFloats(o)))
	os16 := make([]int16, out*fs)
	for row := 0; row < in; row++ {
		mappingMatrixMultiplyChannelOutShort(&m, res[row:], row, in, os16, out, fs)
	}
	var b []byte
	for _, v := range os16 {
		b = binary.LittleEndian.AppendUint16(b, uint16(v))
	}
	parts = append(parts, fmt.Sprintf("os%016x", fnvHash(b)))
	oi := make([]int32, out*fs)
	for row := 0; row < in; row++ {
		mappingMatrixMultiplyChannelOutInt24(&m, res[row:], row, in, oi, out, fs)
	}
	b = b[:0]
	for _, v := range oi {
		b = binary.LittleEndian.AppendUint32(b, uint32(v))
	}
	parts = append(parts, fmt.Sprintf("oi%016x", fnvHash(b)))
	return strings.Join(parts, " ")
}

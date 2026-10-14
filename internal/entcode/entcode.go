// Ported from libopus 1.6.1 celt/entcode.h and celt/entcode.c.

package entcode

const (
	// WindowSize is the number of bits in the raw-bits window
	// (EC_WINDOW_SIZE).
	WindowSize = 32
	// UintBits is the number of bits to use for the range-coded part of
	// unsigned integers (EC_UINT_BITS).
	UintBits = 8
	// BitRes is the resolution of fractional-precision bit usage
	// measurements: 3 means 1/8th bits (BITRES).
	BitRes = 3
)

// Ctx is the entropy encoder and decoder context (ec_ctx). The same
// structure serves both directions, so that Tell and TellFrac work on
// either one. Copying a Ctx copies the state but shares Buf, like a
// struct copy in C.
type Ctx struct {
	// Buf is the buffered input or output.
	Buf []byte
	// Storage is the size of the buffer.
	Storage uint32
	// EndOffs is the offset at which the last byte containing raw bits
	// was read or written.
	EndOffs uint32
	// EndWindow holds bits that will be read from or written at the end.
	EndWindow uint32
	// NendBits is the number of valid bits in EndWindow.
	NendBits int
	// NbitsTotal is the total number of whole bits read or written. It
	// does not include partial bits currently in the range coder.
	NbitsTotal int
	// Offs is the offset at which the next range coder byte will be read
	// or written.
	Offs uint32
	// Rng is the number of values in the current range.
	Rng uint32
	// Val is, in the decoder, the difference between the top of the
	// current range and the input value, minus one. In the encoder it is
	// the low end of the current range.
	Val uint32
	// Ext is, in the decoder, the saved normalization factor from
	// Decode. In the encoder it is the number of outstanding carry
	// propagating symbols.
	Ext uint32
	// Rem is a buffered input or output symbol, awaiting carry
	// propagation.
	Rem int
	// Error is nonzero if an error occurred.
	Error int
}

// RangeBytes returns the number of bytes used by the range coder so far
// (ec_range_bytes).
func (c *Ctx) RangeBytes() uint32 {
	return c.Offs
}

// Buffer returns the buffer (ec_get_buffer).
func (c *Ctx) Buffer() []byte {
	return c.Buf
}

// Tell returns the number of bits "used" by the encoded or decoded
// symbols so far (ec_tell). The encoder and the decoder compute the same
// number, so it is suitable for making coding decisions. It is always
// slightly larger than the exact value.
func (c *Ctx) Tell() int {
	return c.NbitsTotal - Ilog(c.Rng)
}

var tellFracCorrection = [8]uint32{
	35733, 38967, 42495, 46340,
	50535, 55109, 60097, 65535,
}

// TellFrac returns the number of bits "used" by the encoded or decoded
// symbols so far, scaled by 2**BitRes (ec_tell_frac). It is always
// slightly larger than the exact value.
func (c *Ctx) TellFrac() uint32 {
	nbits := uint32(c.NbitsTotal) << BitRes
	l := Ilog(c.Rng)
	r := c.Rng >> uint(l-16)
	b := (r >> 12) - 8
	if r > tellFracCorrection[b] {
		b++
	}
	l = (l << 3) + int(b)
	return nbits - uint32(l)
}

// Udiv returns n/d (celt_udiv). d must be positive.
func Udiv(n, d uint32) uint32 {
	return n / d
}

// Sudiv returns n/d rounded toward zero (celt_sudiv). d must be positive.
func Sudiv(n, d int32) int32 {
	return n / d
}

// Ported from libopus 1.6.1 celt/mfrngcod.h.

package entcode

// Constants used by the entropy encoder and decoder.
const (
	// SymBits is the number of bits to output at a time (EC_SYM_BITS).
	SymBits = 8
	// CodeBits is the total number of bits in each of the state
	// registers (EC_CODE_BITS).
	CodeBits = 32
	// symMax is the maximum symbol value (EC_SYM_MAX).
	symMax = 1<<SymBits - 1
	// codeShift is the number of bits to shift by to move a symbol into
	// the high-order position (EC_CODE_SHIFT).
	codeShift = CodeBits - SymBits - 1
	// codeTop is the carry bit of the high-order range symbol
	// (EC_CODE_TOP).
	codeTop uint32 = 1 << (CodeBits - 1)
	// codeBot is the low-order bit of the high-order range symbol
	// (EC_CODE_BOT).
	codeBot = codeTop >> SymBits
	// codeExtra is the number of bits available for the last, partial
	// symbol in the code field (EC_CODE_EXTRA).
	codeExtra = (CodeBits-2)%SymBits + 1
)

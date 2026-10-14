// Ported from libopus 1.6.1 silk/resampler_rom.c and silk/resampler_rom.h.
// Code generated from the C tables; values are literal copies.

package silk

// resampler3_4COEFS is silk_Resampler_3_4_COEFS.
var resampler3_4COEFS = [29]int16{
	-20694, -13867, -49, 64, 17, -157, 353, -496,
	163, 11047, 22205, -39, 6, 91, -170, 186,
	23, -896, 6336, 19928, -19, -36, 102, -89,
	-24, 328, -951, 2568, 15909,
}

// resampler2_3COEFS is silk_Resampler_2_3_COEFS.
var resampler2_3COEFS = [20]int16{
	-14457, -14019, 64, 128, -122, 36, 310, -768,
	584, 9267, 17733, 12, 128, 18, -142, 288,
	-117, -865, 4123, 14459,
}

// resampler1_2COEFS is silk_Resampler_1_2_COEFS.
var resampler1_2COEFS = [14]int16{
	616, -14323, -10, 39, 58, -46, -84, 120,
	184, -315, -541, 1284, 5380, 9024,
}

// resampler1_3COEFS is silk_Resampler_1_3_COEFS.
var resampler1_3COEFS = [20]int16{
	16102, -15162, -13, 0, 20, 26, 5, -31,
	-43, -4, 65, 90, 7, -157, -248, -44,
	593, 1583, 2612, 3271,
}

// resampler1_4COEFS is silk_Resampler_1_4_COEFS.
var resampler1_4COEFS = [20]int16{
	22500, -15099, 3, -14, -20, -15, 2, 25,
	37, 25, -16, -71, -107, -79, 50, 292,
	623, 982, 1288, 1464,
}

// resampler1_6COEFS is silk_Resampler_1_6_COEFS.
var resampler1_6COEFS = [20]int16{
	27540, -15257, 17, 12, 8, 1, -10, -22,
	-30, -32, -22, 3, 44, 100, 168, 243,
	317, 381, 429, 455,
}

// resampler2_3COEFSLQ is silk_Resampler_2_3_COEFS_LQ.
var resampler2_3COEFSLQ = [6]int16{-2797, -6507, 4697, 10739, 1567, 8276}

// resamplerFracFIR12 is silk_resampler_frac_FIR_12.
var resamplerFracFIR12 = [12][4]int16{
	{189, -600, 617, 30567},
	{117, -159, -1070, 29704},
	{52, 221, -2392, 28276},
	{-4, 529, -3350, 26341},
	{-48, 758, -3956, 23973},
	{-80, 905, -4235, 21254},
	{-99, 972, -4222, 18278},
	{-107, 967, -3957, 15143},
	{-103, 896, -3487, 11950},
	{-91, 773, -2865, 8798},
	{-71, 611, -2143, 5784},
	{-46, 425, -1375, 2996},
}

// Tables from resampler_rom.h.
const (
	resamplerDownOrderFIR0 = 18 // RESAMPLER_DOWN_ORDER_FIR0
	resamplerDownOrderFIR1 = 24 // RESAMPLER_DOWN_ORDER_FIR1
	resamplerDownOrderFIR2 = 36 // RESAMPLER_DOWN_ORDER_FIR2
	resamplerOrderFIR12    = 8  // RESAMPLER_ORDER_FIR_12

	// Tables for 2x downsampler.
	resamplerDown2_0 = 9872          // silk_resampler_down2_0
	resamplerDown2_1 = 39809 - 65536 // silk_resampler_down2_1
)

// resamplerUp2HQ0 is silk_resampler_up2_hq_0 (2x upsampler, high quality).
var resamplerUp2HQ0 = [3]int16{1746, 14986, 39083 - 65536}

// resamplerUp2HQ1 is silk_resampler_up2_hq_1.
var resamplerUp2HQ1 = [3]int16{6854, 25769, 55542 - 65536}

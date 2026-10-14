// Ported from libopus 1.6.1 silk/pitch_est_tables.c.
// Code generated from the C tables; values are literal copies.

package silk

// cbLagsStage2_10Ms is silk_CB_lags_stage2_10_ms.
var cbLagsStage2_10Ms = [2][3]int8{
	{0, 1, 0},
	{0, 0, 1},
}

// cbLagsStage3_10Ms is silk_CB_lags_stage3_10_ms.
var cbLagsStage3_10Ms = [2][12]int8{
	{0, 0, 1, -1, 1, -1, 2, -2, 2, -2, 3, -3},
	{0, 1, 0, 1, -1, 2, -1, 2, -2, 3, -2, 3},
}

// lagRangeStage3_10Ms is silk_Lag_range_stage3_10_ms.
var lagRangeStage3_10Ms = [2][2]int8{
	{-3, 7},
	{-2, 7},
}

// cbLagsStage2 is silk_CB_lags_stage2.
var cbLagsStage2 = [4][11]int8{
	{0, 2, -1, -1, -1, 0, 0, 1, 1, 0, 1},
	{0, 1, 0, 0, 0, 0, 0, 1, 0, 0, 0},
	{0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 0},
	{0, -1, 2, 1, 0, 1, 1, 0, 0, -1, -1},
}

// cbLagsStage3 is silk_CB_lags_stage3.
var cbLagsStage3 = [4][34]int8{
	{0, 0, 1, -1, 0, 1, -1, 0, -1, 1, -2, 2, -2, -2, 2, -3, 2, 3, -3, -4, 3, -4, 4, 4, -5, 5, -6, -5, 6, -7, 6, 5, 8, -9},
	{0, 0, 1, 0, 0, 0, 0, 0, 0, 0, -1, 1, 0, 0, 1, -1, 0, 1, -1, -1, 1, -1, 2, 1, -1, 2, -2, -2, 2, -2, 2, 2, 3, -3},
	{0, 1, 0, 0, 0, 0, 0, 0, 1, 0, 1, 0, 0, 1, -1, 1, 0, 0, 2, 1, -1, 2, -1, -1, 2, -1, 2, 2, -1, 3, -2, -2, -2, 3},
	{0, 1, 0, 0, 1, 0, 1, -1, 2, -1, 2, -1, 2, 3, -2, 3, -2, -2, 4, 4, -3, 5, -3, -4, 6, -4, 6, 5, -5, 8, -6, -5, -7, 9},
}

// lagRangeStage3 is silk_Lag_range_stage3.
var lagRangeStage3 = [3][4][2]int8{
	{
		{-5, 8},
		{-1, 6},
		{-1, 6},
		{-4, 10},
	},
	{
		{-6, 10},
		{-2, 6},
		{-1, 6},
		{-5, 10},
	},
	{
		{-9, 12},
		{-3, 7},
		{-2, 7},
		{-7, 13},
	},
}

// nbCbkSearchsStage3 is silk_nb_cbk_searchs_stage3.
var nbCbkSearchsStage3 = [3]int8{16, 24, 34}

// Package celt is a port of the CELT layer of libopus 1.6.1 in its
// default configuration (float build, FLOAT_APPROX; no ENABLE_DEEP_PLC,
// ENABLE_DRED, ENABLE_OSCE, ENABLE_QEXT or CUSTOM_MODES): the CELT
// encoder, the CELT decoder, and the code that they share.
//
// # Files
//
//	modes.go         celt/modes.c (static 48 kHz mode, eband5ms, band_allocation)
//	celt.go          celt/celt.c and celt/celt.h (comb_filter, init_caps, tables)
//	bands.go         celt/bands.c (quant_all_bands for both directions)
//	quant_bands.go   celt/quant_bands.c (both directions)
//	rate.go          celt/rate.c (clt_compute_allocation, both directions)
//	vq.go            celt/vq.c (alg_quant, alg_unquant, op_pvq_search_c, ...)
//	cwrs.go          celt/cwrs.c (encode_pulses, decode_pulses)
//	cwrs_tables.go   CELT_PVQ_U_DATA, generated from celt/cwrs.c
//	celt_decoder.go  celt/celt_decoder.c
//	celt_encoder.go  celt/celt_encoder.c
//
// # Mapping of C names to Go names
//
//	CELTMode, OpusCustomMode     Mode
//	opus_custom_mode_create      CustomModeCreate (StaticMode for the only mode)
//	CELTDecoder                  Decoder
//	celt_decoder_init            NewDecoder, Decoder.Init
//	celt_decode_with_ec          Decoder.DecodeWithEC
//	celt_decode_with_ec_dred     Decoder.DecodeWithEC
//	OPUS_RESET_STATE             Decoder.Reset
//	OPUS_SET/GET_COMPLEXITY      Decoder.SetComplexity, Decoder.Complexity
//	CELT_SET_START_BAND          Decoder.SetStartBand
//	CELT_SET_END_BAND            Decoder.SetEndBand
//	CELT_SET_CHANNELS            Decoder.SetChannels
//	CELT_GET_AND_CLEAR_ERROR     Decoder.GetAndClearError
//	OPUS_GET_LOOKAHEAD           Decoder.Lookahead
//	OPUS_GET_PITCH               Decoder.Pitch
//	CELT_GET_MODE                Decoder.Mode
//	CELT_SET_SIGNALLING          Decoder.SetSignalling
//	OPUS_GET_FINAL_RANGE         Decoder.FinalRange
//	OPUS_SET/GET_PHASE_INVERSION_DISABLED
//	                             Decoder.SetPhaseInversionDisabled,
//	                             Decoder.PhaseInversionDisabled
//	CELTEncoder                  Encoder
//	celt_encoder_init            NewEncoder, Encoder.Init
//	celt_encode_with_ec          Encoder.EncodeWithEC
//	celt_preemphasis             Preemphasis
//	OPUS_RESET_STATE             Encoder.Reset
//	OPUS_SET_COMPLEXITY          Encoder.SetComplexity
//	CELT_SET_START_BAND          Encoder.SetStartBand
//	CELT_SET_END_BAND            Encoder.SetEndBand
//	CELT_SET_PREDICTION          Encoder.SetPrediction
//	OPUS_SET_PACKET_LOSS_PERC    Encoder.SetPacketLossPerc
//	OPUS_SET_VBR                 Encoder.SetVBR
//	OPUS_SET_VBR_CONSTRAINT      Encoder.SetVBRConstraint
//	OPUS_SET_BITRATE             Encoder.SetBitrate (OPUS_BITRATE_MAX is BitrateMax)
//	CELT_SET_CHANNELS            Encoder.SetChannels
//	OPUS_SET/GET_LSB_DEPTH       Encoder.SetLSBDepth, Encoder.LSBDepth
//	OPUS_SET/GET_PHASE_INVERSION_DISABLED
//	                             Encoder.SetPhaseInversionDisabled,
//	                             Encoder.PhaseInversionDisabled
//	CELT_SET_SIGNALLING          Encoder.SetSignalling
//	CELT_SET_ANALYSIS            Encoder.SetAnalysis
//	CELT_SET_SILK_INFO           Encoder.SetSILKInfo
//	OPUS_SET_LFE                 Encoder.SetLFE
//	OPUS_SET_ENERGY_MASK         Encoder.SetEnergyMask
//	CELT_GET_MODE                Encoder.Mode
//	OPUS_GET_FINAL_RANGE         Encoder.FinalRange
//	resampling_factor            ResamplingFactor
//	OPUS_OK, OPUS_BAD_ARG, ...   OK, BadArg, ...
//
// The shared functions keep their C names in camel case and are
// unexported, for the CELT encoder in this package: quant_all_bands is
// quantAllBands, quant_coarse_energy is quantCoarseEnergy,
// clt_compute_allocation is cltComputeAllocation, and so on. A function
// that both encodes and decodes takes a rangeCoder (the C ec_ctx) with
// exactly one of its encoder and decoder set.
//
// # Differences from C
//
//   - Arrays that C passes as pointers are slices; C pointer offsets are
//     slice offsets or explicit index arguments (combFilter takes the
//     index of x[0] because it reads x[-T-2]).
//   - The encoder and decoder states are fixed-size structs for up to two
//     channels. Scratch buffers that C puts on the stack live in the
//     Encoder and Decoder, so encoding and decoding do not allocate.
//   - Return codes are the C int codes (OK, BadArg, InternalError).
//   - The C asserts (celt_assert, validate_celt_decoder,
//     validate_celt_encoder) are not ported. A pcm slice shorter than
//     frameSize*channels panics. Without CUSTOM_MODES the C encoder
//     asserts that the signalling CTL is 0; the Go encoder does not
//     check it (opus_encoder.c always sets it to 0).
//   - Encoder.SetEnergyMask keeps a reference to the slice, like the C
//     pointer. Encoder.Reset clears it, the analysis info and the SILK
//     info, as OPUS_RESET_STATE does in C.
//   - Float products are wrapped in float32 conversions to prevent FMA
//     fusion, which keeps the output bit-exact on every architecture.
//
// # Packet loss concealment
//
// The decoder follows the side of every ENABLE_DEEP_PLC and ENABLE_DRED
// conditional that the default build compiles: celt_decode_with_ec_dred
// takes no LPCNetPLCState, and celt_decode_lost uses only the noise-based
// (FRAME_PLC_NOISE) and pitch-based (FRAME_PLC_PERIODIC) concealment.
// There is no update_plc_state, no 16 kHz neural PLC buffer and no DRED
// reset of plc_duration. The decoder complexity is stored and returned,
// but it does not change the output.
package celt

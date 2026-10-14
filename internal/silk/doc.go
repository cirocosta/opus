// Package silk is a port of the libopus 1.6.1 SILK codec (silk/ and
// silk/float/): the decoder, the float-build encoder, and the code they
// share (tables, NLSF, LPC helpers, the fixed-point macros and the
// resampler). The SILK decoder is fixed-point code in every libopus
// build, so this port is integer arithmetic that matches libopus bit
// for bit. The encoder is the float build (silk/float/*_FLP.c around the
// fixed-point quantizers); it keeps the C operation order and float or
// double precision of every expression, and matches libopus bit for bit
// too.
//
// The port follows the default libopus configuration: ENABLE_DEEP_PLC,
// ENABLE_OSCE, ENABLE_OSCE_BWE and ENABLE_DRED are not defined, so the
// code takes the not-defined side of every such #ifdef in silk/. The
// decoder has no LPCNet, OSCE or bandwidth extension hooks, and
// silk_PLC_glue_frames always applies the energy fade after a lost
// frame (the ENABLE_DEEP_PLC build skips it at 16 kHz).
//
// # Files and names
//
// Each Go file ports the C file with the same name in lower case
// (NLSF2A.c becomes nlsf2a.go, tables_NLSF_CB_WB.c becomes
// tables_nlsf_cb_wb.go). Shared headers: SigProc_FIX.h and typedef.h are
// sigproc_fix.go, macros.h is macros.go, Inlines.h is inlines.go,
// define.h, errors.h and pitch_est_defines.h are define.go, structs.h
// and resampler_structs.h are structs.go. Every identifier names its C
// counterpart in its doc comment.
//
// Naming rules, so that new code (the encoder) can find the shared
// pieces:
//
//   - Functions drop the silk_ prefix and become lower camel case:
//     silk_NLSF_decode is nlsfDecode, silk_LPC_inverse_pred_gain is
//     lpcInversePredGain, silk_resampler_init is resamplerInit.
//     Functions that take silk_decoder_state *psDec are methods on
//     *decoderState (silk_decode_core is psDec.decodeCore).
//   - Macros become small functions with the macro name in lower case:
//     silk_SMULWB is smulwb, silk_SMLAWB is smlawb, silk_RSHIFT_ROUND is
//     rshiftRound (rshiftRound64 for the 64-bit form), silk_ADD32_ovflw
//     is add32ovflw, silk_SAT16 is sat16, silk_LSHIFT is lshift,
//     silk_LIMIT is limit32 (limitInt for opus_int), silk_RAND is
//     silkRAND. Plain silk_ADD32/silk_SUB32/silk_RSHIFT are written as Go
//     operators on int32, which wrap like the C code expects.
//   - Tables drop silk_ and become lower camel case with initialisms
//     kept: silk_NLSF_CB_WB is nlsfCBWB, silk_gain_iCDF is gainICDF,
//     silk_LTP_gain_iCDF_ptrs is ltpGainICDFPtrs, silk_Resampler_3_4_COEFS
//     is resampler3_4COEFS. The large tables were generated from the C
//     sources and are literal copies.
//   - Structs: silk_decoder_state is decoderState, silk_decoder_control
//     is decoderControl, silk_NLSF_CB_struct is nlsfCBStruct,
//     SideInfoIndices is sideInfoIndices, silk_resampler_state_struct is
//     resamplerState. Field names are lower camel case versions of the C
//     fields (prev_gain_Q16 is prevGainQ16).
//
// Encoder names follow the same rules: silk_encoder_state is
// encoderState, silk_encoder_state_FLP is encoderStateFLP (functions that
// take it are methods, silk_encode_frame_FLP is psEnc.encodeFrameFLP),
// silk_encoder_control_FLP is encoderControlFLP, silk_nsq_state is
// nsqState, silk_VAD_state is vadState, silk_LP_state is lpState,
// stereo_enc_state is stereoEncState. Float files keep the _FLP suffix as
// a lower case _flp (find_LPC_FLP.c is find_lpc_flp.go) and the function
// names keep FLP (silk_burg_modified_FLP is burgModifiedFLP).
// SigProc_FLP.h is sigproc_flp.go, structs_FLP.h is structs_flp.go and
// tuning_parameters.h is tuning_parameters.go. C pointer arithmetic into
// a buffer becomes a slice plus an explicit offset argument where the C
// code indexes backwards (x[ -k ]).
//
// # API
//
// Decoder is silk_decoder. Init is silk_InitDecoder, Reset is
// silk_ResetDecoder, and Decode is silk_Decode with DecControl as
// silk_DecControlStruct. Decode writes opus_res samples (float32 scaled
// by 1/32768, INT16TORES), as the float build does; DecodeInt16 returns
// the opus_int16 samples before that conversion. The arch argument is
// dropped. silk_LoadOSCEModels is a no-op in this configuration and is
// not ported. DecControl has no enable_deep_plc field: opus_decoder.c
// sets it, but only ENABLE_DEEP_PLC code reads it (through
// silk_PLC_struct.enable_deep_plc, which is not ported either).
//
// Encoder is silk_encoder (the super struct of structs_FLP.h). Init is
// silk_InitEncoder and Encode is silk_Encode with EncControl as
// silk_EncControlStruct, including the LBRR_coded, useDRED, maxBits,
// toMono, opusCanSwitch and reducedDependency inputs and the
// switchReady, internalSampleRate, inWBmodeWithoutVariableLP,
// stereoWidth_Q14, signalType and offset outputs. Encode takes opus_res
// input (float32 scaled by 1/32768), as the float build does, and
// converts it with RES2INT16 (FLOAT2INT16). The arch argument is dropped.
// Encode does not allocate: the C stack buffers are scratch arrays in the
// encoder state.
//
// The SILK encoder calls no DNN code; DRED lives in opus_encoder.c.
// EncControl.UseDRED is carried but not read, as in C. In this
// configuration opus_encoder.c never sets it (only
// OPUS_SET_DRED_DURATION does, under ENABLE_DRED), so it stays 0.
//
// # Deviations
//
//   - silk_InitDecoder resets nChannelsAPI and nChannelsInternal, as
//     opus_decoder_init does by clearing the whole decoder first.
//   - The C asserts are not ported. The reference build defines
//     ENABLE_HARDENING, so libopus aborts on, for example, an invalid
//     EncControl (check_control_input asserts before returning the
//     error); Encode returns the error code instead.
//   - Encoder.Init clears both channel states. silk_InitEncoder leaves
//     the second one untouched for a mono encoder, which never uses it.
//   - Encoder float code: the libm calls (exp in silk_sigmoid, log10 in
//     silk_log2, pow and sqrt) use Go's math package. After the float
//     conversions that follow them they match glibc 2.43 exactly on 4M
//     point sweeps of each, and on all integer pitch lags. The sign of a
//     NaN can differ from C when an intermediate value is not finite,
//     because Go may swap the operands of commutative float operations;
//     this does not happen for int16-range input.
package silk

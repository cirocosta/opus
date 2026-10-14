// Package opus is a pure Go port of libopus 1.6.1, the reference
// implementation of the Opus audio codec (RFC 6716, RFC 8251).
//
// The port follows the default floating-point build of libopus (plain
// ./configure: FLOAT_APPROX and ENABLE_RES24 on; no DRED, deep PLC or
// OSCE), and produces the same packets and samples. It needs no cgo and
// no assembly.
//
// # Encoder and decoder
//
// [Encoder] (opus_encoder.c) encodes 16-bit ([Encoder.Encode]), 24-bit
// ([Encoder.Encode24]) or float ([Encoder.EncodeFloat]) PCM at 8, 12, 16,
// 24 or 48 kHz into one packet per call. Frames are 2.5 to 120 ms long.
// The libopus encoder controls are methods: SetBitrate, SetComplexity,
// SetVBR, SetInbandFEC, SetPacketLossPerc, SetDTX, SetSignal,
// SetBandwidth and others; FinalRange, Lookahead and InDTX report state.
//
// [Decoder] (opus_decoder.c) decodes packets to PCM, conceals lost
// packets with the classic SILK and CELT concealment, and decodes in-band
// FEC. As in the default libopus build, the decoder complexity is stored
// but does not change the output.
//
// # Multistream and projection
//
// [MSEncoder] and [MSDecoder] code several mono and stereo streams in one
// packet, with explicit channel mappings ([NewMSEncoder]) or the standard
// mapping families 0, 1 (Vorbis order with surround masking), 2
// (ambisonics) and 255 ([NewMSSurroundEncoder]).
// [ProjectionEncoder] and [ProjectionDecoder] code ambisonics with the
// mixing and demixing matrices of mapping family 3.
//
// # Packet utilities
//
// [PacketParse], [PacketGetNbSamples] and the other Packet functions
// inspect packets; [Repacketizer] merges and splits packets; [PacketPad]
// and [PacketUnpad] change padding; [PacketExtensionsParse] and
// [PacketExtensionsGenerate] handle padding extensions.
//
// Encoders and decoders are not safe for concurrent use. They allocate
// their state once; encoding and decoding do not allocate.
package opus

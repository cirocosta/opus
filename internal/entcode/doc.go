// Package entcode is a port of the libopus 1.6.1 range coder
// (celt/entcode, celt/entenc, celt/entdec, celt/mfrngcod.h,
// celt/ecintrin.h) and the Laplace coder (celt/laplace).
//
// The C code uses one struct, ec_ctx, for both directions. This package
// uses one struct, Ctx, that Encoder and Decoder embed. The fields of
// ec_ctx are exported fields of Ctx with the same meaning, because celt,
// silk, and the top-level codec read or adjust some of them directly
// (for example the final range, the storage size, and the error flag).
//
// Mapping of C names to Go names:
//
//	ec_ctx                       Ctx (embedded in Encoder and Decoder)
//	ec_ctx.buf                   Ctx.Buf
//	ec_ctx.storage               Ctx.Storage
//	ec_ctx.end_offs              Ctx.EndOffs
//	ec_ctx.end_window            Ctx.EndWindow
//	ec_ctx.nend_bits             Ctx.NendBits
//	ec_ctx.nbits_total           Ctx.NbitsTotal
//	ec_ctx.offs                  Ctx.Offs
//	ec_ctx.rng                   Ctx.Rng
//	ec_ctx.val                   Ctx.Val
//	ec_ctx.ext                   Ctx.Ext
//	ec_ctx.rem                   Ctx.Rem
//	ec_ctx.error                 Ctx.Error
//	ec_range_bytes               Ctx.RangeBytes
//	ec_get_buffer                Ctx.Buffer
//	ec_get_error                 Ctx.Error (read the field)
//	ec_tell                      Ctx.Tell
//	ec_tell_frac                 Ctx.TellFrac
//	EC_ILOG, ec_ilog             Ilog
//	celt_udiv                    Udiv
//	celt_sudiv                   Sudiv
//	BITRES                       BitRes
//	EC_UINT_BITS                 UintBits
//	EC_WINDOW_SIZE               WindowSize
//	EC_SYM_BITS                  SymBits
//	EC_CODE_BITS                 CodeBits
//
//	ec_enc_init                  Encoder.Init
//	ec_encode                    Encoder.Encode
//	ec_encode_bin                Encoder.EncodeBin
//	ec_enc_bit_logp              Encoder.EncBitLogp
//	ec_enc_icdf                  Encoder.EncIcdf
//	ec_enc_icdf16                Encoder.EncIcdf16
//	ec_enc_uint                  Encoder.EncUint
//	ec_enc_bits                  Encoder.EncBits
//	ec_enc_patch_initial_bits    Encoder.PatchInitialBits
//	ec_enc_shrink                Encoder.Shrink
//	ec_enc_done                  Encoder.Done
//	ec_laplace_encode            Encoder.LaplaceEncode
//	ec_laplace_encode_p0         Encoder.LaplaceEncodeP0
//
//	ec_dec_init                  Decoder.Init
//	ec_decode                    Decoder.Decode
//	ec_decode_bin                Decoder.DecodeBin
//	ec_dec_update                Decoder.Update
//	ec_dec_bit_logp              Decoder.DecBitLogp
//	ec_dec_icdf                  Decoder.DecIcdf
//	ec_dec_icdf16                Decoder.DecIcdf16
//	ec_dec_uint                  Decoder.DecUint
//	ec_dec_bits                  Decoder.DecBits
//	ec_laplace_decode            Decoder.LaplaceDecode
//	ec_laplace_decode_p0         Decoder.LaplaceDecodeP0
//
// Differences from C:
//
//   - Init takes a slice and sets Storage to len(buf). To code into a
//     prefix of a buffer, pass a shorter slice.
//   - ec_laplace_encode returns the coded value through an int pointer.
//     Encoder.LaplaceEncode returns it instead.
//   - The C asserts (celt_assert) are not ported. Invalid arguments that
//     would be undefined behavior in C can panic in Go.
package entcode

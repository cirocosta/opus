/* Hybrid replay logger: runs the full reference opus decoder on a test
   vector and logs every call into the CELT decoder (via ld --wrap), with
   the range decoder state, so a Go test can replay the CELT side of hybrid
   frames bit-exactly. Links libopus.a of the default configuration (no
   ENABLE_DEEP_PLC, ENABLE_DRED, ENABLE_OSCE). */
#include "config.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <stdarg.h>
#include "opus.h"
#include "celt.h"
#include "entdec.h"

static FILE *logf_;
static const unsigned char *cur_pkt;
static int cur_len, cur_idx;
static int ncalls;

static void w32(int32_t v) { fwrite(&v, 4, 1, logf_); }

static uint64_t fnv(const void *p, size_t n) {
  const unsigned char *b = p; size_t i; uint64_t h = 1469598103934665603ULL;
  for (i = 0; i < n; i++) { h ^= b[i]; h *= 1099511628211ULL; }
  return h;
}

int __real_celt_decode_with_ec_dred(CELTDecoder *st, const unsigned char *data, int len, opus_res *pcm, int frame_size, ec_dec *dec, int accum);
int __real_celt_decode_with_ec(CELTDecoder *st, const unsigned char *data, int len, opus_res *pcm, int frame_size, ec_dec *dec, int accum);
int __real_opus_custom_decoder_ctl(CELTDecoder *st, int request, ...);

static float tmp[2 * 960 * 2];

static int logged_decode(CELTDecoder *st, const unsigned char *data, int len, opus_res *pcm, int frame_size, ec_dec *dec, int accum, int dred) {
  int off, ret, i, CC;
  opus_uint32 rng;
  float *out;
  if (data == NULL) off = -1;
  else if (data >= cur_pkt && data < cur_pkt + cur_len) off = (int)(data - cur_pkt);
  else if (len == 2 && data[0] == 0xFF && data[1] == 0xFF) off = -2;
  else { fprintf(stderr, "unknown data pointer\n"); exit(1); }
  w32(2); w32(cur_idx); w32(off); w32(len); w32(frame_size); w32(accum); w32(dred); w32(dec != NULL);
  if (dec) {
    w32(dec->storage); w32(dec->end_offs); w32(dec->end_window); w32(dec->nend_bits);
    /* ec_dec_init leaves ext uninitialized and ec_decode sets it before
       any read, so log 0 to keep the log deterministic. */
    w32(dec->nbits_total); w32(dec->offs); w32(dec->rng); w32(dec->val); w32(0);
    w32(dec->rem); w32(dec->error);
  }
  out = accum ? tmp : pcm;
  if (accum) memset(tmp, 0, sizeof(tmp));
  if (dred) ret = __real_celt_decode_with_ec_dred(st, data, len, out, frame_size, dec, accum);
  else ret = __real_celt_decode_with_ec(st, data, len, out, frame_size, dec, accum);
  __real_opus_custom_decoder_ctl(st, OPUS_GET_FINAL_RANGE_REQUEST, &rng);
  CC = 0;
  {
    /* channels: count from the frame size and buffer is unknown; the
       driver passes it through a global. */
    extern int g_channels;
    CC = g_channels;
  }
  w32(ret); w32(rng);
  {
    uint64_t h = fnv(out, ret > 0 ? sizeof(float) * ret * CC : 0);
    w32((int32_t)(uint32_t)h); w32((int32_t)(uint32_t)(h >> 32));
  }
  if (accum && ret > 0) for (i = 0; i < ret * CC; i++) pcm[i] += tmp[i];
  ncalls++;
  return ret;
}

int g_channels;

int __wrap_celt_decode_with_ec_dred(CELTDecoder *st, const unsigned char *data, int len, opus_res *pcm, int frame_size, ec_dec *dec, int accum) {
  return logged_decode(st, data, len, pcm, frame_size, dec, accum, 1);
}
int __wrap_celt_decode_with_ec(CELTDecoder *st, const unsigned char *data, int len, opus_res *pcm, int frame_size, ec_dec *dec, int accum) {
  return logged_decode(st, data, len, pcm, frame_size, dec, accum, 0);
}

int __wrap_opus_custom_decoder_ctl(CELTDecoder *st, int request, ...) {
  va_list ap;
  int ret;
  va_start(ap, request);
  switch (request) {
    case OPUS_RESET_STATE:
      va_end(ap);
      w32(1); w32(request); w32(0);
      return __real_opus_custom_decoder_ctl(st, request);
    case CELT_SET_SIGNALLING_REQUEST: case CELT_SET_END_BAND_REQUEST:
    case CELT_SET_CHANNELS_REQUEST: case CELT_SET_START_BAND_REQUEST:
    case OPUS_SET_COMPLEXITY_REQUEST: case OPUS_SET_PHASE_INVERSION_DISABLED_REQUEST: {
      opus_int32 v = va_arg(ap, opus_int32);
      va_end(ap);
      w32(1); w32(request); w32(v);
      return __real_opus_custom_decoder_ctl(st, request, v);
    }
    default: {
      void *p = va_arg(ap, void *);
      va_end(ap);
      ret = __real_opus_custom_decoder_ctl(st, request, p);
      return ret;
    }
  }
}

int main(int argc, char **argv) {
  FILE *f;
  long n, p = 0;
  unsigned char *b;
  int err, fs, CC;
  OpusDecoder *dec;
  static opus_int16 out[5760 * 2];
  if (argc != 5) { fprintf(stderr, "usage: hyb file.bit fs ch log\n"); return 2; }
  fs = atoi(argv[2]); CC = atoi(argv[3]); g_channels = CC;
  f = fopen(argv[1], "rb"); fseek(f, 0, SEEK_END); n = ftell(f); fseek(f, 0, SEEK_SET);
  b = malloc(n); if (fread(b, 1, n, f) != (size_t)n) return 1; fclose(f);
  logf_ = fopen(argv[4], "wb");
  w32(0); w32(fs); w32(CC);
  dec = opus_decoder_create(fs, CC, &err);
  while (p < n) {
    int len = (b[p] << 24) | (b[p + 1] << 16) | (b[p + 2] << 8) | b[p + 3];
    cur_pkt = b + p + 8; cur_len = len; cur_idx++;
    p += 8 + len;
    if (opus_decode(dec, len ? cur_pkt : NULL, len, out, 5760, 0) < 0) { /* errors are part of the log */ }
  }
  fclose(logf_);
  printf("calls=%d\n", ncalls);
  return 0;
}

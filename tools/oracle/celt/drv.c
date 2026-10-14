/* CELT decoder golden driver. Links libopus.a of the default
   configuration (no ENABLE_DEEP_PLC, ENABLE_DRED, ENABLE_OSCE), built
   with --disable-intrinsics --disable-rtcd --disable-asm and
   -ffp-contract=off. The Go tests in internal/celt mirror each mode. */
#include "config.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include "opus.h"
#include "celt.h"
#include "modes.h"
#include "entdec.h"

/* ---- hashing ---- */
static uint64_t H = 1469598103934665603ULL;
static FILE *dumpf = NULL;
static void hbytes(const void *p, size_t n) {
  const unsigned char *b = p;
  size_t i;
  for (i = 0; i < n; i++) { H ^= b[i]; H *= 1099511628211ULL; }
  if (dumpf) fwrite(p, 1, n, dumpf);
}
static void hi32(int32_t v) { hbytes(&v, 4); }
static void hu32(uint32_t v) { hbytes(&v, 4); }

static uint32_t lcg(uint32_t x) { return x * 1664525u + 1013904223u; }

/* loss pattern: returns 1 when packet p is lost */
static int lost(int pattern, int p) {
  uint32_t r;
  if (pattern == 0) return 0;
  if (pattern == 1) {
    r = lcg((uint32_t)p * 2654435761u);
    if (((r >> 16) % 100) < 15) return 1;
    if ((p % 97) >= 50 && (p % 97) < 62) return 1;
    return 0;
  }
  /* pattern 2: long bursts */
  if ((p % 211) >= 100 && (p % 211) < 160) return 1;
  return 0;
}

static int endband_for(int toc) {
  int bw = (toc >> 5) & 3; /* for CELT configs 16..31: 0 NB, 1 WB, 2 SWB, 3 FB */
  static const int eb[4] = {13, 17, 19, 21};
  return eb[bw];
}

static unsigned char *readall(const char *path, long *n) {
  FILE *f = fopen(path, "rb");
  unsigned char *b;
  if (!f) { perror(path); exit(1); }
  fseek(f, 0, SEEK_END); *n = ftell(f); fseek(f, 0, SEEK_SET);
  b = malloc(*n);
  if (fread(b, 1, *n, f) != (size_t)*n) exit(1);
  fclose(f);
  return b;
}

static float pcm[2 * 960 * 6];

static void decode_frame(CELTDecoder *st, const unsigned char *data, int len, int frame_size, int accum, int CC) {
  int ret, i;
  opus_uint32 rng;
  if (accum) {
    for (i = 0; i < frame_size * CC; i++) pcm[i] = (float)((i * 7) % 13 - 6) * (1.f / 1024);
  }
  ret = celt_decode_with_ec(st, data, len, pcm, frame_size, NULL, accum);
  celt_decoder_ctl(st, OPUS_GET_FINAL_RANGE(&rng));
  hi32(ret);
  hu32(rng);
  if (ret > 0) hbytes(pcm, sizeof(float) * ret * CC);
}

/* vec mode: decode CELT-only frames of a test vector. */
static int run_vec(const char *path, int CC, int fs, int pattern, int accum, int cx) {
  long n, p = 0;
  unsigned char *f = readall(path, &n);
  int err, idx = 0, frames = 0, rngok = 0, rngtot = 0;
  CELTDecoder *st = malloc(celt_decoder_get_size(CC));
  int ds;
  if (celt_decoder_init(st, fs, CC) != OPUS_OK) return 1;
  celt_decoder_ctl(st, CELT_SET_SIGNALLING(0));
  ds = 48000 / fs;
  if (celt_decoder_ctl(st, OPUS_SET_COMPLEXITY(cx)) != OPUS_OK) return 1;
  while (p < n) {
    int len = (f[p] << 24) | (f[p + 1] << 16) | (f[p + 2] << 8) | f[p + 3];
    uint32_t frng = ((uint32_t)f[p + 4] << 24) | (f[p + 5] << 16) | (f[p + 6] << 8) | f[p + 7];
    const unsigned char *pkt = f + p + 8;
    unsigned char toc;
    const unsigned char *frm[48];
    opus_int16 sizes[48];
    int nf, k, fsz;
    p += 8 + len;
    idx++;
    if (len == 0) continue;
    toc = pkt[0];
    if ((toc >> 3) < 16) continue;
    nf = opus_packet_parse(pkt, len, &toc, frm, sizes, NULL);
    if (nf < 0) continue;
    fsz = (120 << ((toc >> 3) & 3)) / ds;
    celt_decoder_ctl(st, CELT_SET_END_BAND(endband_for(toc)));
    celt_decoder_ctl(st, CELT_SET_CHANNELS((toc & 4) ? 2 : 1));
    if (lost(pattern, idx)) {
      for (k = 0; k < nf; k++) { decode_frame(st, NULL, 0, fsz, accum, CC); frames++; }
      continue;
    }
    for (k = 0; k < nf; k++) { decode_frame(st, frm[k], sizes[k], fsz, accum, CC); frames++; }
    if (!pattern && fs == 48000) {
      opus_uint32 rng;
      celt_decoder_ctl(st, OPUS_GET_FINAL_RANGE(&rng));
      rngtot++;
      if (rng == frng) rngok++;
    }
  }
  (void)err;
  printf("hash=%016llx frames=%d rngok=%d/%d\n", (unsigned long long)H, frames, rngok, rngtot);
  return 0;
}

/* signal generator shared with the Go test */
static float gen_sample(uint32_t *seed, int i, int c, int kind) {
  float t = (float)i;
  float v;
  *seed = lcg(*seed);
  v = (float)((int32_t)*seed >> 8) * (1.f / 8388608.f);
  if (kind == 0) return 0.3f * v;
  if (kind == 2 && (i / 960) % 10 >= 7) return 0;
  /* tonal + noise, deterministic float ops only (no libm) */
  {
    int ph = (i * (37 + 11 * c)) % 400;
    float tri = (float)(ph < 200 ? ph : 400 - ph) * (1.f / 100.f) - 1.f;
    return 0.5f * tri + 0.05f * v + ((i / 4800) % 2 ? 0.f : 0.2f * v);
  }
  (void)t;
}

/* enc mode: produce CELT packets with the reference encoder and write
   them to a file: for each frame: uint16 len, bytes. */
static int run_enc(const char *out, int CC, int start, int bitrate, int fsz, int nframes, int complexity, int kind, int vbr) {
  int err, i, j;
  CELTEncoder *enc = malloc(celt_encoder_get_size(CC));
  FILE *fo = fopen(out, "wb");
  uint32_t seed = 12345;
  float in[2 * 960];
  unsigned char buf[1275];
  if (celt_encoder_init(enc, 48000, CC, 0) != OPUS_OK) return 1;
  celt_encoder_ctl(enc, CELT_SET_SIGNALLING(0));
  celt_encoder_ctl(enc, CELT_SET_START_BAND(start));
  celt_encoder_ctl(enc, OPUS_SET_BITRATE(bitrate));
  celt_encoder_ctl(enc, OPUS_SET_COMPLEXITY(complexity));
  celt_encoder_ctl(enc, OPUS_SET_VBR(vbr));
  for (i = 0; i < nframes; i++) {
    int nb;
    unsigned char l2[2];
    int bytes = bitrate * fsz / 48000 / 8;
    if (bytes > 1275) bytes = 1275;
    for (j = 0; j < fsz; j++) {
      int c;
      for (c = 0; c < CC; c++) in[j * CC + c] = gen_sample(&seed, i * fsz + j, c, kind);
    }
    nb = celt_encode_with_ec(enc, in, fsz, buf, bytes, NULL);
    if (nb < 0) { fprintf(stderr, "enc err %d\n", nb); return 1; }
    l2[0] = nb >> 8; l2[1] = nb & 255;
    fwrite(l2, 1, 2, fo);
    fwrite(buf, 1, nb, fo);
  }
  fclose(fo);
  (void)err;
  return 0;
}

/* pk mode: decode a packet file written by run_enc. */
static int run_pk(const char *path, int CC, int SC, int start, int fsz, int fs, int pattern, int cx) {
  long n, p = 0;
  unsigned char *f = readall(path, &n);
  int idx = 0, frames = 0;
  CELTDecoder *st = malloc(celt_decoder_get_size(CC));
  if (celt_decoder_init(st, fs, CC) != OPUS_OK) return 1;
  celt_decoder_ctl(st, CELT_SET_SIGNALLING(0));
  celt_decoder_ctl(st, CELT_SET_START_BAND(start));
  celt_decoder_ctl(st, CELT_SET_CHANNELS(SC));
  if (celt_decoder_ctl(st, OPUS_SET_COMPLEXITY(cx)) != OPUS_OK) return 1;
  while (p < n) {
    int len = (f[p] << 8) | f[p + 1];
    const unsigned char *pkt = f + p + 2;
    p += 2 + len;
    idx++;
    if (lost(pattern, idx)) decode_frame(st, NULL, 0, fsz * fs / 48000, 0, CC);
    else decode_frame(st, pkt, len, fsz * fs / 48000, 0, CC);
    frames++;
  }
  printf("hash=%016llx frames=%d\n", (unsigned long long)H, frames);
  return 0;
}

/* rnd mode: decode random packets with random parameters. */
static int run_rnd(uint32_t seed, int n, int CC, int fs) {
  int i, j, frames = 0;
  CELTDecoder *st = malloc(celt_decoder_get_size(CC));
  static const int eb[4] = {13, 17, 19, 21};
  unsigned char buf[1275];
  if (celt_decoder_init(st, fs, CC) != OPUS_OK) return 1;
  celt_decoder_ctl(st, CELT_SET_SIGNALLING(0));
  for (i = 0; i < n; i++) {
    int len, LM, start, end, sc, err;
    seed = lcg(seed); len = (seed >> 8) % 400;
    seed = lcg(seed); LM = (seed >> 8) % 4;
    seed = lcg(seed); sc = 1 + ((seed >> 8) & 1);
    seed = lcg(seed); start = ((seed >> 8) % 4) == 0 ? 17 : 0;
    seed = lcg(seed); end = start ? eb[2 + ((seed >> 8) & 1)] : eb[(seed >> 8) & 3];
    for (j = 0; j < len; j++) { seed = lcg(seed); buf[j] = seed >> 24; }
    celt_decoder_ctl(st, CELT_SET_START_BAND(start));
    celt_decoder_ctl(st, CELT_SET_END_BAND(end));
    celt_decoder_ctl(st, CELT_SET_CHANNELS(sc));
    decode_frame(st, len < 2 ? NULL : buf, len, (120 << LM) * fs / 48000, 0, CC);
    celt_decoder_ctl(st, CELT_GET_AND_CLEAR_ERROR(&err));
    hi32(err);
    if ((seed >> 8) % 50 == 0) celt_decoder_ctl(st, OPUS_RESET_STATE);
    frames++;
  }
  printf("hash=%016llx frames=%d\n", (unsigned long long)H, frames);
  return 0;
}

int main(int argc, char **argv) {
  const char *dump = getenv("CELT_DUMP");
  if (dump) dumpf = fopen(dump, "wb");
  if (argc >= 2 && !strcmp(argv[1], "vec") && argc == 8)
    return run_vec(argv[2], atoi(argv[3]), atoi(argv[4]), atoi(argv[5]), atoi(argv[6]), atoi(argv[7]));
  if (argc >= 2 && !strcmp(argv[1], "enc") && argc == 11)
    return run_enc(argv[2], atoi(argv[3]), atoi(argv[4]), atoi(argv[5]), atoi(argv[6]), atoi(argv[7]), atoi(argv[8]), atoi(argv[9]), atoi(argv[10]));
  if (argc >= 2 && !strcmp(argv[1], "pk") && argc == 10)
    return run_pk(argv[2], atoi(argv[3]), atoi(argv[4]), atoi(argv[5]), atoi(argv[6]), atoi(argv[7]), atoi(argv[8]), atoi(argv[9]));
  if (argc >= 2 && !strcmp(argv[1], "rnd") && argc == 6)
    return run_rnd((uint32_t)strtoul(argv[2], 0, 10), atoi(argv[3]), atoi(argv[4]), atoi(argv[5]));
  fprintf(stderr, "usage\n");
  return 2;
}

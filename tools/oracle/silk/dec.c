/* SILK decoder golden driver for internal/silk (see testdata/README).

   Decodes the SILK part of every SILK-only and hybrid frame of an Opus
   stream in the RFC 8251 test vector format with silk_Decode and prints
   one line per silk_Decode call:

     frame lostFlag ret nSamplesOut finalRange prevPitchLag fnv1a64(samples)

   The hash covers the opus_res (float) output samples. Build against a
   libopus 1.6.1 tree configured with the defaults (no DRED, deep PLC or
   OSCE); compile with -include <tree>/config.h so that the control
   structures have the layout of that build (see gen.sh). */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <math.h>
#include "opus.h"
#include "entdec.h"
#include "API.h"
#include "control.h"

#if defined(ENABLE_DEEP_PLC) || defined(ENABLE_OSCE) || defined(ENABLE_DRED)
#error "build against a libopus tree without DRED, deep PLC and OSCE"
#endif

static uint64_t fnv(uint64_t h, const void *p, size_t n) {
  const unsigned char *b = p;
  for (size_t i = 0; i < n; i++) { h ^= b[i]; h *= 1099511628211ULL; }
  return h;
}

typedef struct { unsigned char *data; int len; } frame_t;

int main(int argc, char **argv) {
  if (argc < 6) {
    fprintf(stderr, "usage: dec in.bit api_rate api_ch loss(0,1,2) out.txt [samples.raw]\n");
    return 1;
  }
  int api_rate = atoi(argv[2]), api_ch = atoi(argv[3]), loss = atoi(argv[4]);
  FILE *fin = fopen(argv[1], "rb");
  FILE *fout = fopen(argv[5], "w");
  FILE *fraw = argc > 6 ? fopen(argv[6], "wb") : NULL;
  if (!fin || !fout || (argc > 6 && !fraw)) { fprintf(stderr, "open failed\n"); return 1; }
  /* Read packets and split them into Opus frames. */
  int cap = 1 << 16, nfr = 0;
  frame_t *frs = calloc(cap, sizeof(frame_t));
  int *tocs = calloc(cap, sizeof(int));
  unsigned char hdr[8];
  while (fread(hdr, 1, 8, fin) == 8) {
    int len = (hdr[0] << 24) | (hdr[1] << 16) | (hdr[2] << 8) | hdr[3];
    unsigned char *pkt = malloc(len > 0 ? len : 1);
    if ((int)fread(pkt, 1, len, fin) != len) break;
    if (len <= 0) continue;
    const unsigned char *f[48]; opus_int16 sz[48]; unsigned char toc;
    int n = opus_packet_parse(pkt, len, &toc, f, sz, NULL);
    if (n <= 0) continue;
    for (int i = 0; i < n && nfr < cap; i++) {
      frs[nfr].data = (unsigned char *)f[i]; frs[nfr].len = sz[i]; tocs[nfr] = toc; nfr++;
    }
  }
  int size; silk_Get_Decoder_Size(&size);
  void *dec = calloc(1, size);
  silk_InitDecoder(dec);
  silk_DecControlStruct ctl; memset(&ctl, 0, sizeof(ctl));
  ctl.API_sampleRate = api_rate; ctl.nChannelsAPI = api_ch;
  float out[2 * 960];
  uint32_t x = 12345;
  int prev_celt = 0;
  for (int fi = 0; fi < nfr; fi++) {
    int toc = tocs[fi], cfg = toc >> 3;
    if (cfg >= 16) { prev_celt = 1; continue; }
    if (prev_celt) { silk_ResetDecoder(dec); prev_celt = 0; }
    int dur = cfg < 12 ? (10 << (cfg & 3)) : (10 << (cfg & 1));
    if (dur == 80) dur = 60;
    /* Loss simulation: an LCG marks 15% of the frames lost. */
    x = x * 1103515245u + 12345u;
    int lost = loss != 0 && ((x >> 16) % 100) < 15;
    int lostFlag = 0;
    frame_t *src = &frs[fi];
    int stoc = toc;
    if (lost) {
      lostFlag = 1;
      if (loss == 2 && fi + 1 < nfr && (tocs[fi + 1] >> 3) < 16 && frs[fi + 1].len > 1) {
        lostFlag = 2; src = &frs[fi + 1]; stoc = tocs[fi + 1];
      }
    } else if (src->len <= 1) {
      lostFlag = 1;
    }
    if (lostFlag != 1) {
      int scfg = stoc >> 3;
      ctl.nChannelsInternal = ((stoc >> 2) & 1) + 1;
      ctl.internalSampleRate = scfg < 12 ? (scfg < 4 ? 8000 : scfg < 8 ? 12000 : 16000) : 16000;
      int sdur = scfg < 12 ? (10 << (scfg & 3)) : (10 << (scfg & 1));
      if (sdur == 80) sdur = 60;
      dur = sdur;
    }
    if (ctl.nChannelsInternal == 0) continue; /* nothing decoded yet */
    ctl.payloadSize_ms = dur;
    ec_dec ec;
    if (lostFlag != 1) ec_dec_init(&ec, src->data, src->len);
    else memset(&ec, 0, sizeof(ec));
    int total = dur * api_rate / 1000, decoded = 0, first = 1;
    while (decoded < total) {
      opus_int32 n = 0;
      int ret = silk_Decode(dec, &ctl, lostFlag, first, &ec, out, &n, 0);
      uint64_t h = fnv(14695981039346656037ULL, out, (size_t)n * api_ch * sizeof(float));
      fprintf(fout, "%d %d %d %d %u %d %016llx\n", fi, lostFlag, ret, (int)n,
              lostFlag != 1 ? ec.rng : 0u, ctl.prevPitchLag, (unsigned long long)h);
      if (fraw) {
        /* int16 little-endian (the samples before INT16TORES). */
        for (int i = 0; i < n * api_ch; i++) {
          int16_t s = (int16_t)lrintf(out[i] * 32768.0f);
          unsigned char b[2] = { (unsigned char)(s & 0xFF), (unsigned char)((uint16_t)s >> 8) };
          fwrite(b, 1, 2, fraw);
        }
      }
      if (ret || n <= 0) break;
      decoded += n; first = 0;
    }
  }
  fclose(fout);
  if (fraw) fclose(fraw);
  return 0;
}

/* Reference driver for the Go port of the libopus decoder API
   (opus_decoder_test.go). Links the default-configuration libopus.a.
   Modes:
     drv dec [opts] in.bit         opus_demo-style decode loop
     drv msenc fs ch family bitrate frames framesize out.pk
     drv msdec [opts] in.pk        multistream / projection decode
   Options: -fs rate -ch channels -fmt 16|24|f -cx complexity -gain g
   -loss pct -seed s -reset frame -plc 2.5ms-units -fec -ignext -phinv.
   Every decoder call prints one line; the Go test prints the same. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <math.h>
#include "opus.h"
#include "opus_multistream.h"
#include "opus_projection.h"

static unsigned long long fnv(const void *p, size_t n) {
  const unsigned char *b = p; unsigned long long h = 14695981039346656037ULL;
  for (size_t i = 0; i < n; i++) { h ^= b[i]; h *= 1099511628211ULL; }
  return h;
}

static unsigned int lcg;
static int lcg_lost(int pct) {
  lcg = lcg*1103515245u + 12345u;
  return pct > 0 && (int)((lcg>>16)%100) < pct;
}

typedef struct { int fs, ch, fmt, cx, gain, loss, seed, fec, ignext, reset, phinv, plc; } opts;

static int parse_opts(int argc, char **argv, opts *o) {
  int i;
  memset(o, 0, sizeof(*o));
  o->fs = 48000; o->ch = 2; o->fmt = 16; o->reset = -1; o->seed = 1;
  for (i = 2; i < argc - 1; i++) {
    if (!strcmp(argv[i], "-fs")) o->fs = atoi(argv[++i]);
    else if (!strcmp(argv[i], "-ch")) o->ch = atoi(argv[++i]);
    else if (!strcmp(argv[i], "-fmt")) { i++; o->fmt = argv[i][0]=='f' ? 0 : atoi(argv[i]); }
    else if (!strcmp(argv[i], "-cx")) o->cx = atoi(argv[++i]);
    else if (!strcmp(argv[i], "-gain")) o->gain = atoi(argv[++i]);
    else if (!strcmp(argv[i], "-loss")) o->loss = atoi(argv[++i]);
    else if (!strcmp(argv[i], "-seed")) o->seed = atoi(argv[++i]);
    else if (!strcmp(argv[i], "-reset")) o->reset = atoi(argv[++i]);
    else if (!strcmp(argv[i], "-plc")) o->plc = atoi(argv[++i]);
    else if (!strcmp(argv[i], "-fec")) o->fec = 1;
    else if (!strcmp(argv[i], "-ignext")) o->ignext = 1;
    else if (!strcmp(argv[i], "-phinv")) o->phinv = 1;
    else { fprintf(stderr, "bad opt %s\n", argv[i]); exit(1); }
  }
  lcg = o->seed;
  return argc - 1;
}

static unsigned int be32(const unsigned char *p) { return (unsigned)p[0]<<24|p[1]<<16|p[2]<<8|p[3]; }

#define MAXF 5760
static opus_int16 o16[MAXF*255];
static opus_int32 o24[MAXF*255];
static float of[MAXF*255];

static unsigned long long hash_out(int fmt, int n) {
  if (n <= 0) return 0;
  if (fmt == 16) return fnv(o16, n*2);
  if (fmt == 24) return fnv(o24, n*4);
  return fnv(of, n*4);
}

static int do_dec(OpusDecoder *dec, int fmt, const unsigned char *d, int len, int fs, int fec) {
  if (fmt == 16) return opus_decode(dec, d, len, o16, fs, fec);
  if (fmt == 24) return opus_decode24(dec, d, len, o24, fs, fec);
  return opus_decode_float(dec, d, len, of, fs, fec);
}

static int mode_dec(int argc, char **argv) {
  opts o; int err, idx = 0, lost_count = 0;
  FILE *f; unsigned char hdr[8], data[1500];
  OpusDecoder *dec;
  parse_opts(argc, argv, &o);
  f = fopen(argv[argc-1], "rb");
  if (!f) { perror("open"); return 1; }
  dec = opus_decoder_create(o.fs, o.ch, &err);
  if (!dec) { fprintf(stderr, "create %d\n", err); return 1; }
  opus_decoder_ctl(dec, OPUS_SET_COMPLEXITY(o.cx));
  if (o.gain) opus_decoder_ctl(dec, OPUS_SET_GAIN(o.gain));
  if (o.ignext) opus_decoder_ctl(dec, OPUS_SET_IGNORE_EXTENSIONS(1));
  if (o.phinv) opus_decoder_ctl(dec, OPUS_SET_PHASE_INVERSION_DISABLED(1));
  while (fread(hdr, 1, 8, f) == 8) {
    int len = be32(hdr), lost, run, fr;
    if (len > 1500 || fread(data, 1, len, f) != (size_t)len) break;
    lost = len == 0 || lcg_lost(o.loss);
    if (lost) { lost_count++; run = 0; } else run = 1 + lost_count;
    for (fr = 0; fr < run; fr++) {
      opus_int32 os = 0, rng = 0, dur = 0, bw = 0, pitch = 0; int ret, kind;
      if (fr == lost_count-1 && o.fec && opus_packet_has_lbrr(data, len) > 0) {
        opus_decoder_ctl(dec, OPUS_GET_LAST_PACKET_DURATION(&os));
        if (o.plc) os = o.plc*o.fs/400;
        ret = do_dec(dec, o.fmt, data, len, os, 1); kind = 1;
      } else if (fr < lost_count) {
        opus_decoder_ctl(dec, OPUS_GET_LAST_PACKET_DURATION(&os));
        if (o.plc) os = o.plc*o.fs/400;
        ret = do_dec(dec, o.fmt, NULL, 0, os, 0); kind = 3;
      } else {
        ret = do_dec(dec, o.fmt, data, len, MAXF*o.fs/48000, 0); kind = 0;
      }
      opus_decoder_ctl(dec, OPUS_GET_FINAL_RANGE((opus_uint32*)&rng));
      opus_decoder_ctl(dec, OPUS_GET_LAST_PACKET_DURATION(&dur));
      opus_decoder_ctl(dec, OPUS_GET_BANDWIDTH(&bw));
      opus_decoder_ctl(dec, OPUS_GET_PITCH(&pitch));
      printf("%d %d %d %08x %d %d %d %016llx\n", idx, kind, ret, (unsigned)rng, dur, bw, pitch,
             hash_out(o.fmt, ret*o.ch));
    }
    if (!lost) lost_count = 0;
    if (idx == o.reset) opus_decoder_ctl(dec, OPUS_RESET_STATE);
    idx++;
  }
  opus_decoder_destroy(dec);
  return 0;
}

/* Packet file for multistream/projection:
   "OPMS" type(1) fs(4) ch(1) streams(1) coupled(1)
   type 0: mapping[ch]; type 1: matrix_size(4) matrix
   then packets: len(4 BE) data */
static void put32(FILE *f, unsigned v) { unsigned char b[4]={v>>24,v>>16,v>>8,v}; fwrite(b,1,4,f); }

static int mode_msenc(int argc, char **argv) {
  int fs, ch, family, bitrate, frames, fsz, err, streams, coupled, i, c, n;
  unsigned char mapping[256], pkt[1500*255];
  FILE *f; OpusMSEncoder *ms = NULL; OpusProjectionEncoder *pe = NULL;
  float *in; double t = 0;
  if (argc != 9) { fprintf(stderr, "usage\n"); return 1; }
  fs = atoi(argv[2]); ch = atoi(argv[3]); family = atoi(argv[4]);
  bitrate = atoi(argv[5]); frames = atoi(argv[6]); fsz = atoi(argv[7]);
  f = fopen(argv[8], "wb");
  fwrite("OPMS", 1, 4, f);
  if (family == 3) {
    pe = opus_projection_ambisonics_encoder_create(fs, ch, 3, &streams, &coupled, OPUS_APPLICATION_AUDIO, &err);
    if (!pe) { fprintf(stderr, "proj create %d\n", err); return 1; }
    opus_projection_encoder_ctl(pe, OPUS_SET_BITRATE(bitrate));
  } else {
    ms = opus_multistream_surround_encoder_create(fs, ch, family, &streams, &coupled, mapping, OPUS_APPLICATION_AUDIO, &err);
    if (!ms) { fprintf(stderr, "ms create %d\n", err); return 1; }
    opus_multistream_encoder_ctl(ms, OPUS_SET_BITRATE(bitrate));
  }
  fputc(family == 3, f); put32(f, fs); fputc(ch, f); fputc(streams, f); fputc(coupled, f);
  if (family == 3) {
    opus_int32 sz; unsigned char mat[65536];
    opus_projection_encoder_ctl(pe, OPUS_PROJECTION_GET_DEMIXING_MATRIX_SIZE(&sz));
    opus_projection_encoder_ctl(pe, OPUS_PROJECTION_GET_DEMIXING_MATRIX(mat, sz));
    put32(f, sz); fwrite(mat, 1, sz, f);
  } else fwrite(mapping, 1, ch, f);
  in = malloc(sizeof(float)*fsz*ch);
  for (n = 0; n < frames; n++) {
    for (i = 0; i < fsz; i++, t += 1.0/fs) {
      for (c = 0; c < ch; c++) {
        double fr = 200.0*(c+1) + 50.0*sin(t*(0.5+c*0.1));
        double am = 0.3*(0.5+0.5*sin(t*(1.0+0.3*c)+c));
        in[i*ch+c] = (float)(am*sin(2*M_PI*fr*t) + 0.02*((double)(lcg = lcg*1103515245u+12345u)/4294967296.0-0.5));
      }
    }
    if (pe) err = opus_projection_encode_float(pe, in, fsz, pkt, sizeof(pkt));
    else err = opus_multistream_encode_float(ms, in, fsz, pkt, sizeof(pkt));
    if (err < 0) { fprintf(stderr, "enc %d\n", err); return 1; }
    put32(f, err); fwrite(pkt, 1, err, f);
  }
  fclose(f);
  fprintf(stderr, "streams %d coupled %d\n", streams, coupled);
  return 0;
}

static int mode_msdec(int argc, char **argv) {
  opts o; int err, idx = 0, type, fs, ch, streams, coupled;
  FILE *f; unsigned char hdr[16], mapping[256], *mat = NULL, data[1500*255];
  OpusMSDecoder *ms = NULL; OpusProjectionDecoder *pd = NULL; opus_int32 msz = 0;
  parse_opts(argc, argv, &o);
  f = fopen(argv[argc-1], "rb");
  if (fread(hdr, 1, 12, f) != 12 || memcmp(hdr, "OPMS", 4)) return 1;
  type = hdr[4]; fs = be32(hdr+5); ch = hdr[9]; streams = hdr[10]; coupled = hdr[11];
  if (o.fs) fs = o.fs;
  if (type == 1) {
    unsigned char b[4]; if (fread(b, 1, 4, f) != 4) return 1; msz = be32(b);
    mat = malloc(msz); if (fread(mat, 1, msz, f) != (size_t)msz) return 1;
    pd = opus_projection_decoder_create(fs, ch, streams, coupled, mat, msz, &err);
  } else {
    if (fread(mapping, 1, ch, f) != (size_t)ch) return 1;
    ms = opus_multistream_decoder_create(fs, ch, streams, coupled, mapping, &err);
  }
  if (!ms && !pd) { fprintf(stderr, "dec create %d\n", err); return 1; }
#define MSCTL(x) (pd ? opus_projection_decoder_ctl(pd, x) : opus_multistream_decoder_ctl(ms, x))
  MSCTL(OPUS_SET_COMPLEXITY(o.cx));
  if (o.gain) MSCTL(OPUS_SET_GAIN(o.gain));
  if (o.phinv) MSCTL(OPUS_SET_PHASE_INVERSION_DISABLED(1));
  for (;;) {
    unsigned char b[4]; int len, ret, lost, fsz;
    opus_uint32 rng = 0; opus_int32 dur = 0, bw = 0;
    if (fread(b, 1, 4, f) != 4) break;
    len = be32(b);
    if (fread(data, 1, len, f) != (size_t)len) break;
    lost = lcg_lost(o.loss);
    fsz = lost ? fs/50 : MAXF*fs/48000;
    if (lost) len = 0;
    if (pd) {
      if (o.fmt == 16) ret = opus_projection_decode(pd, data, len, o16, fsz, 0);
      else if (o.fmt == 24) ret = opus_projection_decode24(pd, data, len, o24, fsz, 0);
      else ret = opus_projection_decode_float(pd, data, len, of, fsz, 0);
    } else {
      if (o.fmt == 16) ret = opus_multistream_decode(ms, data, len, o16, fsz, 0);
      else if (o.fmt == 24) ret = opus_multistream_decode24(ms, data, len, o24, fsz, 0);
      else ret = opus_multistream_decode_float(ms, data, len, of, fsz, 0);
    }
    MSCTL(OPUS_GET_FINAL_RANGE(&rng));
    MSCTL(OPUS_GET_LAST_PACKET_DURATION(&dur));
    MSCTL(OPUS_GET_BANDWIDTH(&bw));
    printf("%d %d %08x %d %d %016llx\n", idx, ret, (unsigned)rng, dur, bw, hash_out(o.fmt, ret*ch));
    if (idx == o.reset) MSCTL(OPUS_RESET_STATE);
    idx++;
  }
  return 0;
}

int main(int argc, char **argv) {
  if (argc < 3) { fprintf(stderr, "usage: drv dec|msenc|msdec ...\n"); return 1; }
  if (!strcmp(argv[1], "dec")) return mode_dec(argc, argv);
  if (!strcmp(argv[1], "msenc")) return mode_msenc(argc, argv);
  if (!strcmp(argv[1], "msdec")) return mode_msdec(argc, argv);
  return 1;
}

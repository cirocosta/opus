/* Golden driver for the CELT encoder (celt_encode_with_ec). Links
   libopus.a of the default configuration (see build.sh). Reads case
   specs (one per line) from argv[1], int16 stereo PCM from argv[2], and
   prints each spec followed by "=> frames=N bytes=B hash=H pk0=HEX".
   With argv[3] = "v" it prints one line per frame instead. The Go test
   encoder_test.go mirrors it step by step. */
#include "config.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include "celt.h"
#include "modes.h"
#include "entenc.h"
#include "opus_defines.h"

static uint64_t H;
static void hb(const void *p, size_t n) {
  const unsigned char *b = p; size_t i;
  for (i = 0; i < n; i++) { H ^= b[i]; H *= 1099511628211ULL; }
}
static void hi(int32_t v) { hb(&v, 4); }

static uint32_t R;
static uint32_t rnd(void) { R = R * 1664525u + 1013904223u; return R >> 8; }

static int16_t *PCM; static long PCMN; /* stereo frames */

typedef struct {
  char name[64];
  char src[16];
  long off; int ch, fs, lm, n, br, vbr, cvbr, cx, pred, loss, start, end, lsb, lfe, mask, ana, inv, ext, nb, sc, rnd, lmvar, seed;
  long tone;
} Spec;

static int geti(const char *line, const char *key, int def) {
  char k[32]; const char *p; size_t kl;
  snprintf(k, sizeof k, " %s=", key); kl = strlen(k);
  p = strstr(line, k);
  if (!p) return def;
  return (int)strtol(p + kl, NULL, 10);
}
static long getl(const char *line, const char *key, long def) {
  char k[32]; const char *p; size_t kl;
  snprintf(k, sizeof k, " %s=", key); kl = strlen(k);
  p = strstr(line, k);
  if (!p) return def;
  return strtol(p + kl, NULL, 10);
}

static const int brs[] = {6000, 12000, 16000, 24000, 32000, 48000, 64000, 96000, 128000, 192000, 256000, 320000, 510000, -1};
static const int ends[] = {13, 17, 19, 21};
static const int offs[] = {60, 100, 140, 240};

typedef struct { int br, vbr, cvbr, cx, lm, pred, loss, reset, end, sc, nb; } P;

static void perturb(P *p, const Spec *s) {
  int hyb = s->start != 0;
  if (rnd() % 8 == 0) p->br = brs[rnd() % 14];
  if (rnd() % 16 == 0) p->vbr ^= 1;
  if (rnd() % 16 == 0) p->cvbr ^= 1;
  if (rnd() % 8 == 0) p->cx = rnd() % 11;
  if (s->lmvar && rnd() % 6 == 0) p->lm = rnd() % 4;
  if (rnd() % 32 == 0) p->pred = rnd() % 3;
  if (rnd() % 32 == 0) p->loss = rnd() % 30;
  p->reset = rnd() % 97 == 0;
  if (rnd() % 16 == 0) p->end = hyb ? ends[2 + rnd() % 2] : ends[rnd() % 4];
  if (s->ch == 2 && rnd() % 24 == 0) p->sc = 1 + rnd() % 2;
  if (rnd() % 16 == 0) p->nb = 2 + rnd() % 400;
}

/* sample t (stereo frame index) of the source, channel c */
static int64_t ty1, ty0; static long tpos;
static int src_sample(const Spec *s, long t, int c, int frame) {
  int v;
  if (!strcmp(s->src, "sil")) return 0;
  if (!strcmp(s->src, "tone")) {
    while (tpos < t) {
      int64_t y = ((2 * s->tone * ty0) >> 30) - ty1;
      ty1 = ty0; ty0 = y; tpos++;
    }
    v = (int)ty0; /* y[t] */
    if (c == 1) v >>= 1;
    if (v > 32767) v = 32767;
    if (v < -32768) v = -32768;
    return v;
  }
  if (!strcmp(s->src, "gap") && frame % 23 >= 18) return 0;
  {
    long i = (s->off + t) % PCMN;
    return PCM[2 * i + c];
  }
}

static float pcmf[2 * 960];
static unsigned char buf[1500];
static float maskv[42];

typedef struct { int op; uint32_t a, b; } Op;

static void run(const char *line, int verbose) {
  Spec s; P p; CELTEncoder *st; int err, f, i, c; long pos = 0; long bytes = 0;
  char pk0[80] = "-";
  memset(&s, 0, sizeof s);
  sscanf(line, "%63s", s.name);
  {
    const char *q = strstr(line, " src=");
    strcpy(s.src, "pcm");
    if (q) sscanf(q + 5, "%15s", s.src);
  }
  s.off = getl(line, "off", 0); s.ch = geti(line, "ch", 2); s.fs = geti(line, "fs", 48000);
  s.lm = geti(line, "lm", 3); s.n = geti(line, "n", 100); s.br = geti(line, "br", -1);
  s.vbr = geti(line, "vbr", 0); s.cvbr = geti(line, "cvbr", 1); s.cx = geti(line, "cx", 10);
  s.pred = geti(line, "pred", 2); s.loss = geti(line, "loss", 0); s.start = geti(line, "start", 0);
  s.end = geti(line, "end", 21); s.lsb = geti(line, "lsb", 24); s.lfe = geti(line, "lfe", 0);
  s.mask = geti(line, "mask", 0); s.ana = geti(line, "ana", 0); s.inv = geti(line, "inv", 0);
  s.ext = geti(line, "ext", 0); s.nb = geti(line, "nb", 1275); s.sc = geti(line, "sc", s.ch);
  s.rnd = geti(line, "rnd", 0); s.lmvar = geti(line, "lmvar", 0); s.seed = geti(line, "seed", 1);
  s.tone = getl(line, "tone", 1071961363);
  R = (uint32_t)s.seed;
  H = 14695981039346656037ULL;
  /* tone state: ty1 = y[tpos-1], ty0 = y[tpos] */
  ty0 = 12000; ty1 = (12000 * s.tone) >> 30; tpos = 0;
  st = malloc(celt_encoder_get_size(s.ch));
  err = celt_encoder_init(st, s.fs, s.ch, 0);
  if (err != OPUS_OK) { printf("%s => init error %d\n", line, err); free(st); return; }
  celt_encoder_ctl(st, CELT_SET_SIGNALLING(0));
  p.br = s.br; p.vbr = s.vbr; p.cvbr = s.cvbr; p.cx = s.cx; p.lm = s.lm; p.pred = s.pred; p.loss = s.loss;
  p.reset = 0; p.end = s.end; p.sc = s.sc; p.nb = s.nb;
  for (f = 0; f < s.n; f++) {
    int up = 48000 / s.fs, fsz, ret, nb;
    uint32_t rng;
    ec_enc enc;
    if (s.rnd) perturb(&p, &s);
    if (s.start != 0) {
      if (p.vbr) p.cvbr = 0; else p.br = -1;
    }
    if (p.reset) celt_encoder_ctl(st, OPUS_RESET_STATE);
    celt_encoder_ctl(st, OPUS_SET_COMPLEXITY(p.cx));
    celt_encoder_ctl(st, CELT_SET_START_BAND(s.start));
    celt_encoder_ctl(st, CELT_SET_END_BAND(p.end));
    celt_encoder_ctl(st, CELT_SET_PREDICTION(p.pred));
    celt_encoder_ctl(st, OPUS_SET_PACKET_LOSS_PERC(p.loss));
    celt_encoder_ctl(st, OPUS_SET_VBR(p.vbr));
    celt_encoder_ctl(st, OPUS_SET_VBR_CONSTRAINT(p.cvbr));
    celt_encoder_ctl(st, OPUS_SET_BITRATE(p.br));
    celt_encoder_ctl(st, CELT_SET_CHANNELS(p.sc));
    celt_encoder_ctl(st, OPUS_SET_LSB_DEPTH(s.lsb));
    celt_encoder_ctl(st, OPUS_SET_PHASE_INVERSION_DISABLED(s.inv));
    celt_encoder_ctl(st, OPUS_SET_LFE(s.lfe));
    if (s.ana) {
      AnalysisInfo a;
      memset(&a, 0, sizeof a);
      a.valid = s.ana == 1;
      a.tonality = (rnd() % 1001) / 1000.f;
      a.tonality_slope = ((int)(rnd() % 2001) - 1000) / 1000.f;
      a.noisiness = (rnd() % 1001) / 1000.f;
      a.activity = (rnd() % 1001) / 1000.f;
      a.music_prob = (rnd() % 1001) / 1000.f;
      a.music_prob_min = (rnd() % 1001) / 1000.f;
      a.music_prob_max = (rnd() % 1001) / 1000.f;
      a.bandwidth = rnd() % 21;
      a.activity_probability = (rnd() % 1001) / 1000.f;
      a.max_pitch_ratio = (rnd() % 1001) / 1000.f;
      for (i = 0; i < LEAK_BANDS; i++) a.leak_boost[i] = rnd() % 256;
      celt_encoder_ctl(st, CELT_SET_ANALYSIS(&a));
    }
    if (s.mask) {
      for (i = 0; i < 42; i++) maskv[i] = ((int)(rnd() % 97) - 72) / 16.f;
      celt_encoder_ctl(st, OPUS_SET_ENERGY_MASK(maskv));
    }
    if (s.start != 0) {
      SILKInfo si;
      si.signalType = rnd() % 3;
      si.offset = offs[rnd() % 4];
      celt_encoder_ctl(st, CELT_SET_SILK_INFO(&si));
    }
    fsz = (120 << p.lm) / up;
    for (i = 0; i < fsz; i++) {
      for (c = 0; c < s.ch; c++) {
        int v;
        if (s.ch == 1) v = (src_sample(&s, pos + i, 0, f) + src_sample(&s, pos + i, 1, f)) >> 1;
        else v = src_sample(&s, pos + i, c, f);
        if (!strcmp(s.src, "tiny")) pcmf[s.ch * i + c] = (float)((int)(rnd() % 3) - 1) * (1.f / 1048576.f);
        else if (!strcmp(s.src, "loud")) pcmf[s.ch * i + c] = INT16TORES(v) * 3.f;
        else if (!strcmp(s.src, "huge")) pcmf[s.ch * i + c] = INT16TORES(v) * 262144.f;
        else pcmf[s.ch * i + c] = INT16TORES(v);
      }
    }
    pos += fsz;
    nb = p.nb;
    if (s.ext == 0) {
      ret = celt_encode_with_ec(st, pcmf, fsz, buf, nb, NULL);
    } else {
      ec_enc_init(&enc, buf, nb);
      if (s.ext == 2) {
        int lim = nb * 4, K;
        if (p.br > 0 && p.vbr && p.br * (120 << p.lm) / 48000 / 2 < lim) lim = p.br * (120 << p.lm) / 48000 / 2;
        K = rnd() % (lim + 1);
        while (ec_tell(&enc) < K) {
          int op = rnd() % 3;
          if (op == 0) { int logp = 1 + rnd() % 15; int v = rnd() & 1; ec_enc_bit_logp(&enc, v, logp); }
          else if (op == 1) { uint32_t ft = 2 + rnd() % 1000; uint32_t v = rnd() % ft; ec_enc_uint(&enc, v, ft); }
          else { uint32_t v = rnd() & 0xFF; ec_enc_bits(&enc, v, 8); }
        }
      }
      ret = celt_encode_with_ec(st, pcmf, fsz, NULL, nb, &enc);
    }
    celt_encoder_ctl(st, OPUS_GET_FINAL_RANGE(&rng));
    hi(ret); hi((int32_t)rng);
    if (ret > 0) { hb(buf, ret); bytes += ret; }
    if (f == 0 && ret > 0) {
      int k, m = ret < 24 ? ret : 24;
      for (k = 0; k < m; k++) sprintf(pk0 + 2 * k, "%02x", buf[k]);
    }
    if (verbose) {
      printf("f=%d lm=%d br=%d vbr=%d cvbr=%d cx=%d nb=%d sc=%d end=%d ret=%d rng=%08x h=%016llx\n", f, p.lm, p.br, p.vbr,
             p.cvbr, p.cx, nb, p.sc, p.end, ret, rng, (unsigned long long)H);
    }
  }
  free(st);
  if (!verbose) printf("%s => frames=%d bytes=%ld hash=%016llx pk0=%s\n", line, s.n, bytes, (unsigned long long)H, pk0);
}

int main(int argc, char **argv) {
  FILE *fp; long sz; char line[1024];
  fp = fopen(argv[2], "rb");
  if (!fp) { perror(argv[2]); return 1; }
  fseek(fp, 0, SEEK_END); sz = ftell(fp); fseek(fp, 0, SEEK_SET);
  PCM = malloc(sz); if (fread(PCM, 1, sz, fp) != (size_t)sz) return 1; fclose(fp);
  PCMN = sz / 4;
  fp = fopen(argv[1], "r");
  if (!fp) { perror(argv[1]); return 1; }
  while (fgets(line, sizeof line, fp)) {
    char *e = strchr(line, '\n'); if (e) *e = 0;
    e = strstr(line, " =>"); if (e) *e = 0;
    if (line[0] == 0 || line[0] == '#') continue;
    run(line, argc > 3);
    fflush(stdout);
  }
  return 0;
}

/* Golden driver for opus/analysis.go: runs run_analysis from the scalar
   libopus 1.6.1 build over int16 stereo 48 kHz source PCM.
   usage:
     drv synth SRC.raw CONFIGS     -> "# cfg" header + one line per call
     drv vec DIR CONFIGS           -> "testvectorNN cfg ncalls hash"
   config: fs C c1 c2 dm gain lsb fsz la nullp resetp seed maxcalls */
#ifdef HAVE_CONFIG_H
#include "config.h"
#endif
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include "opus_custom.h"
#include "analysis.h"
#include "opus_private.h"

typedef struct {
  int fs, C, c1, c2, dm, gain, lsb, fsz, la, nullp, resetp;
  unsigned seed;
  int maxcalls;
} cfg_t;

static uint32_t rng;
static uint32_t next_rand(void) {
  rng = rng * 1664525u + 1013904223u;
  return rng >> 8;
}

static uint32_t fbits(float f) { uint32_t u; memcpy(&u, &f, 4); return u; }

static int fmt_info(char *s, const AnalysisInfo *a) {
  int n = sprintf(s, "%d %08x %08x %08x %08x %08x %08x %08x %d %08x %08x ",
      a->valid, fbits(a->tonality), fbits(a->tonality_slope), fbits(a->noisiness),
      fbits(a->activity), fbits(a->music_prob), fbits(a->music_prob_min),
      fbits(a->music_prob_max), a->bandwidth, fbits(a->activity_probability),
      fbits(a->max_pitch_ratio));
  for (int i = 0; i < LEAK_BANDS; i++) n += sprintf(s + n, "%02x", a->leak_boost[i]);
  return n;
}

static uint64_t fnv(uint64_t h, const char *s, int n) {
  for (int i = 0; i < n; i++) { h ^= (unsigned char)s[i]; h *= 1099511628211ull; }
  return h;
}

/* run one config; if out != NULL print lines, else hash. returns ncalls. */
static int run(const int16_t *src, int nsrc, const cfg_t *c, FILE *out, uint64_t *hash) {
  const CELTMode *mode = opus_custom_mode_create(48000, 960, NULL);
  int dec = 48000 / c->fs, n = nsrc / dec, C = c->C;
  int16_t *b16 = malloc(sizeof(int16_t) * n * C);
  float *bf = malloc(sizeof(float) * n * C);
  int32_t *b24 = malloc(sizeof(int32_t) * n * C);
  for (int i = 0; i < n; i++) {
    int l = src[2 * i * dec], r = src[2 * i * dec + 1];
    int ch[3] = {l, r, (l - r) >> 1};
    if (C == 1) ch[0] = (l + r) >> 1;
    for (int k = 0; k < C; k++) {
      int v = ch[k];
      int idx = i * C + k;
      b16[idx] = (int16_t)v;
      bf[idx] = (float)(v * c->gain) / 32768.f;
      b24[idx] = v * 256 + (int)((((uint32_t)idx * 2654435761u) >> 24) & 0xff);
    }
  }
  static TonalityAnalysisState st;
  tonality_analysis_init(&st, c->fs);
  rng = c->seed;
  static const int sizes[6] = {1, 2, 4, 8, 16, 24};
  int pos = 0, call = 0;
  uint64_t h = 14695981039346656037ull;
  char line[512];
  while (call < c->maxcalls) {
    int frame = c->fsz > 0 ? c->fsz : sizes[next_rand() % 6] * (c->fs / 400);
    int afs = frame + c->la;
    if (pos + afs > n) break;
    if (c->resetp && call > 0 && call % c->resetp == 0) tonality_analysis_reset(&st);
    const void *pcm;
    downmix_func dm;
    if (c->dm == 0) { pcm = b16 + pos * C; dm = downmix_int; }
    else if (c->dm == 1) { pcm = bf + pos * C; dm = downmix_float; }
    else { pcm = b24 + pos * C; dm = downmix_int24; }
    if (c->nullp && call % c->nullp == c->nullp - 1) pcm = NULL;
    AnalysisInfo info;
    memset(&info, 0, sizeof(info));
    run_analysis(&st, mode, pcm, afs, frame, c->c1, c->c2, C, c->fs, c->lsb, dm, &info);
    int len = fmt_info(line, &info);
    line[len++] = '\n';
    if (out) fwrite(line, 1, len, out);
    h = fnv(h, line, len);
    pos += frame;
    call++;
  }
  free(b16); free(bf); free(b24);
  *hash = h;
  return call;
}

static int16_t *load(const char *path, int *nframes) {
  FILE *f = fopen(path, "rb");
  if (!f) { perror(path); exit(1); }
  fseek(f, 0, SEEK_END);
  long sz = ftell(f);
  fseek(f, 0, SEEK_SET);
  unsigned char *raw = malloc(sz);
  if (fread(raw, 1, sz, f) != (size_t)sz) exit(1);
  fclose(f);
  int ns = sz / 2;
  int16_t *s = malloc(sizeof(int16_t) * ns);
  for (int i = 0; i < ns; i++) s[i] = (int16_t)(raw[2 * i] | (raw[2 * i + 1] << 8));
  free(raw);
  *nframes = ns / 2;
  return s;
}

static int read_cfg(FILE *f, cfg_t *c, char *txt) {
  char buf[256];
  while (fgets(buf, sizeof buf, f)) {
    if (buf[0] == '#' || buf[0] == '\n') continue;
    buf[strcspn(buf, "\n")] = 0;
    if (sscanf(buf, "%d %d %d %d %d %d %d %d %d %d %d %u %d", &c->fs, &c->C, &c->c1, &c->c2,
               &c->dm, &c->gain, &c->lsb, &c->fsz, &c->la, &c->nullp, &c->resetp, &c->seed,
               &c->maxcalls) != 13) { fprintf(stderr, "bad cfg: %s\n", buf); exit(1); }
    strcpy(txt, buf);
    return 1;
  }
  return 0;
}

int main(int argc, char **argv) {
  if (argc != 4) { fprintf(stderr, "usage\n"); return 1; }
  FILE *cf = fopen(argv[3], "r");
  if (!cf) { perror(argv[3]); return 1; }
  cfg_t c;
  char txt[256];
  uint64_t h;
  if (!strcmp(argv[1], "synth")) {
    int n;
    int16_t *src = load(argv[2], &n);
    while (read_cfg(cf, &c, txt)) {
      printf("# %s\n", txt);
      run(src, n, &c, stdout, &h);
    }
  } else {
    cfg_t cfgs[64]; char txts[64][256]; int nc = 0;
    while (read_cfg(cf, &cfgs[nc], txts[nc])) nc++;
    for (int v = 1; v <= 12; v++) {
      char path[1024];
      int n;
      snprintf(path, sizeof path, "%s/testvector%02d.dec", argv[2], v);
      int16_t *src = load(path, &n);
      for (int k = 0; k < nc; k++) {
        int calls = run(src, n, &cfgs[k], NULL, &h);
        printf("testvector%02d %s %d %016llx\n", v, txts[k], calls, (unsigned long long)h);
      }
      free(src);
    }
  }
  return 0;
}

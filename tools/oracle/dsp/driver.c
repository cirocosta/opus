/* Golden generator for opus/internal/dsp. Links against the scalar
   libopus 1.6.1 build. Usage: driver <outdir> <mathN>
   All files are little-endian 32-bit words (float bits or int32). */
#ifdef HAVE_CONFIG_H
#include "config.h"
#endif
#include <stdio.h>
#include <stdlib.h>
#include <stdint.h>
#include <string.h>
#include "modes.h"
#include "kiss_fft.h"
#include "mdct.h"
#include "celt_lpc.h"
#include "pitch.h"
#include "mathops.h"
#include "float_cast.h"

static uint32_t rs;
static uint32_t rng(void) { uint32_t x = rs; x ^= x << 13; x ^= x >> 17; x ^= x << 5; return rs = x; }
static float frand(void) { return (float)(int32_t)rng() * (1.0f / 2147483648.0f); }

static FILE *fo;
static char outdir[512];
static void openf(const char *name) {
  char p[1024];
  snprintf(p, sizeof p, "%s/%s", outdir, name);
  fo = fopen(p, "wb");
  if (!fo) { perror(p); exit(1); }
}
static void wf(float v) { fwrite(&v, 4, 1, fo); }
static void wi(int32_t v) { fwrite(&v, 4, 1, fo); }
static void wfs(const float *v, int n) { fwrite(v, 4, n, fo); }

static const CELTMode *mode;

static void gen_fft(void) {
  int s, i;
  kiss_fft_cpx in[480], out[480];
  openf("fft.bin");
  rs = 12345;
  for (s = 0; s < 4; s++) {
    const kiss_fft_state *st = mode->mdct.kfft[s];
    for (i = 0; i < st->nfft; i++) { in[i].r = 1000.f * frand(); in[i].i = 1000.f * frand(); }
    opus_fft_c(st, in, out);
    wfs((float *)out, 2 * st->nfft);
    opus_ifft_c(st, in, out);
    wfs((float *)out, 2 * st->nfft);
  }
  fclose(fo);
}

static const int mdct_cases[][2] = {{0, 1}, {1, 1}, {2, 1}, {3, 1}, {3, 8}, {2, 4}, {1, 2}};

static void gen_mdct(void) {
  int c, i;
  static float in[4000], out[4000];
  openf("mdct.bin");
  rs = 777;
  for (c = 0; c < 7; c++) {
    int shift = mdct_cases[c][0], stride = mdct_cases[c][1];
    int N = mode->mdct.n >> shift, N2 = N >> 1, ov = mode->overlap;
    for (i = 0; i < N2 + ov; i++) in[i] = 20000.f * frand();
    for (i = 0; i < N2 * stride; i++) out[i] = 0;
    clt_mdct_forward_c(&mode->mdct, in, out, mode->window, ov, shift, stride, 0);
    wfs(out, N2 * stride);
    for (i = 0; i < N2 * stride; i++) in[i] = 20000.f * frand();
    for (i = 0; i < N2 + ov; i++) out[i] = 20000.f * frand();
    clt_mdct_backward_c(&mode->mdct, in, out, mode->window, ov, shift, stride, 0);
    wfs(out, N2 + ov);
  }
  fclose(fo);
}

static void gen_lpc(void) {
  int i, t;
  static float x[2100], ac[30], lpc[30], y[2100], num[24], mem[24];
  openf("lpc.bin");
  rs = 4242;
  /* autocorr + lpc */
  for (t = 0; t < 3; t++) {
    int n = t == 2 ? 992 : 1024, lag = t == 2 ? 4 : 24, ov = t == 2 ? 0 : 120;
    float a = 0;
    for (i = 0; i < n; i++) { a = .9f * a + frand(); x[i] = 5000.f * a + 100.f * frand(); }
    int sh = _celt_autocorr(x, ac, mode->window, ov, lag, n, 0);
    wi(sh);
    wfs(ac, lag + 1);
    _celt_lpc(lpc, ac, lag);
    wfs(lpc, lag);
  }
  /* degenerate lpc */
  for (i = 0; i < 25; i++) ac[i] = 0;
  _celt_lpc(lpc, ac, 24);
  wfs(lpc, 24);
  /* fir */
  for (t = 0; t < 2; t++) {
    int n = t ? 203 : 200, ord = 24;
    for (i = 0; i < n + ord; i++) x[i] = 1000.f * frand();
    for (i = 0; i < ord; i++) num[i] = .1f * frand();
    celt_fir_c(x + ord, num, y, n, ord, 0);
    wfs(y, n);
  }
  /* iir */
  for (t = 0; t < 2; t++) {
    int n = t ? 203 : 200, ord = 24;
    for (i = 0; i < n; i++) x[i] = 1000.f * frand();
    for (i = 0; i < ord; i++) num[i] = .05f * frand();
    for (i = 0; i < ord; i++) mem[i] = 100.f * frand();
    celt_iir(x, num, y, n, ord, mem, 0);
    wfs(y, n);
    wfs(mem, ord);
  }
  /* pitch xcorr with odd sizes, inner products */
  {
    float xc[40];
    for (i = 0; i < 100; i++) x[i] = frand();
    for (t = 3; t <= 8; t++) {
      celt_pitch_xcorr_c(x, x + 50, xc, 30 + t, 13, 0);
      wfs(xc, 13);
    }
    float a, b;
    dual_inner_prod_c(x, x + 10, x + 20, 37, &a, &b);
    wf(a); wf(b);
    wf(celt_inner_prod_c(x, x + 33, 41));
  }
  fclose(fo);
}

static void gen_pitch(void) {
  int t, i, c;
  static float sig[2][1984], xlp[992];
  openf("pitch.bin");
  rs = 99;
  for (t = 0; t < 12; t++) {
    int C = 1 + (t & 1);
    float period = 20.f + 37.3f * t;
    int prev_period = t * 50 + 15;
    float prev_gain = .1f * (t % 8);
    for (c = 0; c < C; c++) {
      for (i = 0; i < 1984; i++) {
        float p = (float)(i + 7 * c) / period;
        float v;
        p -= (float)(int)p;
        v = p * p - p + .1666f;
        sig[c][i] = 48000.f * v + 3000.f * frand() * (float)(t >= 10 ? 5 : 1);
      }
    }
    celt_sig *xs[2] = {sig[0], sig[1]};
    pitch_downsample(xs, xlp, 992, C, 2, 0);
    wfs(xlp, 992);
    int pitch;
    pitch_search(xlp + 512, xlp, 960, 1024 - 3 * 15, &pitch, 0);
    wi(pitch);
    pitch = 1024 - pitch;
    float g = remove_doubling(xlp, 1024, 15, 960, &pitch, prev_period, prev_gain, 0);
    wi(pitch);
    wf(g);
  }
  fclose(fo);
}

/* mathops: N inputs per function, inputs generated here and stored too. */
static void gen_math(int N) {
  int i;
  openf(
#ifdef FLOAT_APPROX
      "math_approx.bin"
#else
      "math_libm.bin"
#endif
  );
  wi(N);
  rs = 2024;
  for (i = 0; i < N; i++) {
    /* log2 over a wide positive range: random exponent and mantissa */
    float x = (1.f + .5f * (frand() + 1.f)) * celt_exp2((float)(int)(rng() % 80) - 40.f);
    if (i < 64) x = (float)(i + 1) * .125f;
    wf(x); wf(celt_log2(x)); wf(celt_log(x));
  }
  for (i = 0; i < N; i++) {
    float x = 45.f * frand() - 10.f;
    if (i < 64) x = -60.f + i;
    wf(x); wf(celt_exp2(x)); wf(celt_exp(.5f * x));
  }
  for (i = 0; i < N; i++) {
    float x = 4.f * frand();
    wf(x); wf(celt_cos_norm(x)); wf(celt_cos_norm2(x)); wf(celt_sin(x));
  }
  for (i = 0; i < N; i++) {
    float x = .5f * (frand() + 1.f), y = 2.f * (frand() + 1.f) * x;
    float a = 1e4f * frand(), b = 1e4f * frand();
    if (i < 16) { a = i & 1 ? 0 : 1e-12f; b = i & 2 ? -0.f : 3.f; }
    wf(x); wf(y); wf(a); wf(b);
    wf(celt_atan_norm(x)); wf(celt_atan2p_norm(y, x)); wf(celt_atan2p_norm(x, y)); wf(fast_atan2f(a, b));
  }
  for (i = 0; i < N; i++) {
    float x = (1.f + frand()) * celt_exp2((float)(int)(rng() % 60) - 30.f);
    wf(x); wf(celt_sqrt(x)); wf(celt_rsqrt(x)); wf(celt_rcp(x)); wf(frac_div32(1.5f, x));
  }
  for (i = 0; i < N; i++) {
    float x = 1.2f * frand();
    if (i < 32) x = (float)(i - 16) / 32768.f * 1.5f; /* half-way cases */
    if (i == 32) x = 1e10f;
    if (i == 33) x = -1e10f;
    wf(x); wi(float2int(32768.f * x)); wi(FLOAT2INT16(x)); wi(FLOAT2INT24(x));
  }
  for (i = 0; i < N; i++) {
    uint32_t v = rng() >> (rng() % 32);
    if (v == 0) v = 1;
    wi((int32_t)v); wi((int32_t)isqrt32(v));
    int32_t a = (int32_t)rng(), b = (int32_t)rng();
    wi(a); wi(b); wi(FRAC_MUL16(a, b));
  }
  {
    float v[37];
    rs = 31337;
    for (i = 0; i < 37; i++) v[i] = 100.f * frand();
    wf(celt_maxabs16(v, 37));
    v[5] = -1000.f;
    wf(celt_maxabs16(v, 37));
  }
  fclose(fo);
}

static void gen_tables(void) {
  int s, i;
  openf("tables.bin");
  wi(mode->Fs); wi(mode->overlap); wi(mode->nbEBands); wi(mode->effEBands);
  wfs(mode->preemph, 4);
  wi(mode->maxLM); wi(mode->nbShortMdcts); wi(mode->shortMdctSize); wi(mode->nbAllocVectors);
  wfs(mode->window, mode->overlap);
  for (i = 0; i < mode->nbEBands; i++) wi(mode->logN[i]);
  wi(mode->cache.size);
  for (i = 0; i < 105; i++) wi(mode->cache.index[i]);
  for (i = 0; i < mode->cache.size; i++) wi(mode->cache.bits[i]);
  for (i = 0; i < 168; i++) wi(mode->cache.caps[i]);
  wi(mode->mdct.n); wi(mode->mdct.maxshift);
  for (s = 0; s < 4; s++) {
    const kiss_fft_state *st = mode->mdct.kfft[s];
    wi(st->nfft); wf(st->scale); wi(st->shift);
    for (i = 0; i < 16; i++) wi(st->factors[i]);
    for (i = 0; i < st->nfft; i++) wi(st->bitrev[i]);
  }
  wfs((const float *)mode->mdct.kfft[0]->twiddles, 960);
  wfs(mode->mdct.trig, 1800);
  fclose(fo);
}

int main(int argc, char **argv) {
  int err;
  if (argc < 3) { fprintf(stderr, "usage: driver outdir mathN [mathonly]\n"); return 1; }
  snprintf(outdir, sizeof outdir, "%s", argv[1]);
  mode = opus_custom_mode_create(48000, 960, &err);
  if (!mode) { fprintf(stderr, "mode err %d\n", err); return 1; }
  if (argc < 4) {
    gen_tables();
    gen_fft();
    gen_mdct();
    gen_lpc();
    gen_pitch();
  }
  gen_math(atoi(argv[2]));
  return 0;
}

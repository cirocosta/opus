/* Unit goldens for SILK encoder helpers that the encoder streams do not
   reach: silk_A2NLSF (incl. bandwidth expansion), silk_LPC_inverse_pred_gain_FLP,
   silk_residual_energy_covar_FLP, silk_schur_FLP, silk_autocorrelation_FLP. */
#include <stdio.h>
#include <stdint.h>
#include <string.h>
#include "main_FLP.h"
#include "SigProc_FLP.h"

static uint32_t x = 1;
static uint32_t nx(void) { x = x * 1664525u + 1013904223u; return x; }
static int rn(int n) { return (int)(((uint64_t)(nx() >> 8) * (uint64_t)n) >> 24); }
static uint64_t fnv(uint64_t h, const void *p, size_t n) {
  const unsigned char *b = p;
  for (size_t i = 0; i < n; i++) { h ^= b[i]; h *= 1099511628211ULL; }
  return h;
}
#define FNV0 14695981039346656037ULL
static float rf(int scale) { return (float)((int)(nx() >> 16) - 32768) / (float)scale; }

int main(void) {
  uint64_t h;
  /* A2NLSF */
  h = FNV0; x = 1;
  for (int c = 0; c < 20000; c++) {
    int d = (c & 1) ? 16 : 10;
    opus_int32 a[16]; opus_int16 nlsf[16];
    int sh = 10 + rn(10);
    for (int i = 0; i < d; i++) a[i] = ((opus_int32)nx()) >> sh;
    silk_A2NLSF(nlsf, a, d);
    h = fnv(h, nlsf, 2 * d); h = fnv(h, a, 4 * d);
  }
  printf("a2nlsf %016llx\n", (unsigned long long)h);
  /* LPC_inverse_pred_gain_FLP */
  h = FNV0; x = 2;
  for (int c = 0; c < 20000; c++) {
    int order = 1 + rn(16);
    float A[16];
    for (int i = 0; i < order; i++) A[i] = rf(8192 * (1 + c % 8));
    float g = silk_LPC_inverse_pred_gain_FLP(A, order);
    h = fnv(h, &g, 4);
  }
  printf("invpredgain %016llx\n", (unsigned long long)h);
  /* residual_energy_covar_FLP */
  h = FNV0; x = 3;
  for (int c = 0; c < 20000; c++) {
    int D = 1 + rn(16);
    float wXX[256], wXx[16], cc[16];
    for (int i = 0; i < D; i++) for (int j = i; j < D; j++) wXX[i + j * D] = wXX[j + i * D] = rf(256);
    for (int i = 0; i < D; i++) { wXx[i] = rf(512); cc[i] = rf(16384); }
    float wxx = rf(64);
    float nrg = silk_residual_energy_covar_FLP(cc, wXX, wXx, wxx, D);
    h = fnv(h, &nrg, 4); h = fnv(h, wXX, 4 * D * D);
  }
  printf("rescovar %016llx\n", (unsigned long long)h);
  /* schur_FLP */
  h = FNV0; x = 4;
  for (int c = 0; c < 20000; c++) {
    int order = 1 + rn(24);
    float ac[25], rc[24], sig[64];
    for (int i = 0; i < 64; i++) sig[i] = c % 3 == 0 ? 0 : rf(64);
    silk_autocorrelation_FLP(ac, sig, 64, order + 1, 0);
    float e = silk_schur_FLP(rc, ac, order);
    h = fnv(h, &e, 4); h = fnv(h, rc, 4 * order);
  }
  printf("schur %016llx\n", (unsigned long long)h);
  /* autocorrelation_FLP */
  h = FNV0; x = 5;
  for (int c = 0; c < 20000; c++) {
    int n = 1 + rn(30), cnt = 1 + rn(40);
    float in[30], res[40];
    memset(res, 0, sizeof(res));
    for (int i = 0; i < n; i++) in[i] = rf(128);
    silk_autocorrelation_FLP(res, in, n, cnt, 0);
    h = fnv(h, res, 4 * 40);
  }
  printf("autocorr %016llx\n", (unsigned long long)h);
  return 0;
}

/* Golden driver for the shared encode/decode helpers of bands.c,
   quant_bands.c, rate.c, vq.c and cwrs.c with synthetic inputs. The Go
   test encf_test.go mirrors it step by step. Usage: encf seed n. */
#include "config.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include "celt.h"
#include "modes.h"
#include "bands.h"
#include "quant_bands.h"
#include "rate.h"
#include "vq.h"
#include "entenc.h"
#include "entdec.h"

static uint64_t H;
static FILE *dumpf;
static void hb(const void *p, size_t n) {
  const unsigned char *b = p; size_t i;
  for (i = 0; i < n; i++) { H ^= b[i]; H *= 1099511628211ULL; }
  if (dumpf) fwrite(p, 1, n, dumpf);
}
static void hi(int32_t v) { hb(&v, 4); }
static void hf(const float *x, int n) { hb(x, 4 * (size_t)n); }
static void hfr(const float *x, int C, int nb, int start, int end) { int c; for (c = 0; c < C; c++) hf(x + c * nb + start, end - start); }
static void hiv(const int *x, int n) { int i; for (i = 0; i < n; i++) hi(x[i]); }

static uint32_t S;
static uint32_t rnd(void) { S = S * 1664525u + 1013904223u; return S >> 8; }
static float rndf(void) { S = S * 1664525u + 1013904223u; return (float)((int32_t)S >> 8) * (1.f / 8388608.f); }

static void one_case(const CELTMode *m) {
  int C = 1 + (rnd() & 1);
  int LM = rnd() % 4;
  int M = 1 << LM;
  int N = M * m->shortMdctSize;
  int nb = m->nbEBands;
  int start = (rnd() % 4 == 0) ? 17 : 0;
  int end = start ? (rnd() & 1 ? 19 : 21) : 13 + rnd() % 9;
  int effEnd = end;
  int nbytes = 4 + rnd() % 300;
  int complexity = rnd() % 11;
  int isTransient = LM > 0 ? (rnd() & 1) : 0;
  int shortBlocks = isTransient ? M : 0;
  int two_pass = rnd() & 1, force_intra = (rnd() % 8) == 0, loss_rate = rnd() % 30, lfe = (rnd() % 16) == 0;
  int disable_inv = rnd() & 1;
  float delayedIntra = (float)(rnd() % 64) * 0.25f;
  int intensity = C == 2 ? start + rnd() % (end - start + 1) : 0;
  int dual_stereo = C == 2 ? rnd() & 1 : 0;
  int alloc_trim = rnd() % 11;
  int prev = rnd() % 21, signalBandwidth = rnd() % 21;
  int i, j, c;
  float freq[2 * 960], X[2 * 960], X2[2 * 960];
  float bandE[2 * 21], bandLogE[2 * 21], oldBandE[2 * 21], oldBandE2[2 * 21], error[2 * 21];
  float prev1[2 * 21], prev2[2 * 21];
  int tf_res[21], offsets[21], cap[21], pulses[21], fine_quant[21], fine_priority[21];
  int pulses2[21], fine_quant2[21], fine_priority2[21];
  int spread_weight[21];
  unsigned char collapse[42], collapse2[42];
  unsigned char buf[1275];
  int average = rnd() % 512, hf_average = rnd() % 32, tapset = rnd() % 3, last_dec = rnd() % 4;
  int spread, bits, anti_rsv, codedBands, balance, anti_on = 0, intensity2 = 0, dual2 = 0, codedBands2;
  opus_int32 balance2;
  opus_uint32 seed = rnd(), seed2;
  ec_enc enc;
  ec_dec dec;
  memset(freq, 0, sizeof(freq));
  memset(X, 0, sizeof(X));
  memset(X2, 0, sizeof(X2));
  memset(error, 0, sizeof(error));
  memset(bandE, 0, sizeof(bandE)); memset(bandLogE, 0, sizeof(bandLogE));
  memset(pulses, 0, sizeof(pulses)); memset(fine_quant, 0, sizeof(fine_quant)); memset(fine_priority, 0, sizeof(fine_priority));
  memset(pulses2, 0, sizeof(pulses2)); memset(fine_quant2, 0, sizeof(fine_quant2)); memset(fine_priority2, 0, sizeof(fine_priority2));
  /* spectrum: random values with a random per-band scale */
  for (c = 0; c < C; c++)
    for (i = 0; i < nb; i++) {
      int kind = rnd() % 8;
      float amp = (float)(1 << (rnd() % 16)) * (1.f / 256.f);
      for (j = M * m->eBands[i]; j < M * m->eBands[i + 1]; j++) {
        float v = rndf();
        if (kind == 0) v = 0;
        else if (kind == 1) v = (j & 3) == 0 ? v : 0;
        freq[c * N + j] = v * amp;
      }
    }
  for (i = 0; i < C * nb; i++) {
    oldBandE[i] = (float)((int)(rnd() % 64) - 40) * 0.5f;
    prev1[i] = (float)((int)(rnd() % 64) - 40) * 0.5f;
    prev2[i] = (float)((int)(rnd() % 64) - 40) * 0.5f;
  }
  if (C == 1) for (i = 0; i < nb; i++) { prev1[nb + i] = prev1[i] - 1; prev2[nb + i] = prev2[i] + 1; }
  memcpy(oldBandE2, oldBandE, sizeof(oldBandE));
  compute_band_energies(m, freq, bandE, effEnd, C, LM, 0);
  normalise_bands(m, freq, X, bandE, effEnd, C, M);
  amp2Log2(m, effEnd, end, bandE, bandLogE, C);
  hf(bandE, C * nb); hf(X, C * N); hf(bandLogE, C * nb);

  for (i = 0; i < nb; i++) spread_weight[i] = 1 + rnd() % 4;
  spread = spreading_decision(m, X, &average, last_dec, &hf_average, &tapset, rnd() & 1, effEnd, C, M, spread_weight);
  hi(spread); hi(average); hi(hf_average); hi(tapset);
  {
    static const opus_val16 thr[3] = {2.f, 5.f, 9.f};
    static const opus_val16 hys[3] = {.5f, 1.f, 1.f};
    float v = (float)(rnd() % 48) * 0.25f;
    hi(hysteresis_decision(v, thr, hys, 3, rnd() % 4));
  }

  ec_enc_init(&enc, buf, nbytes);
  quant_coarse_energy(m, start, end, effEnd, bandLogE, oldBandE, nbytes * 8, error, &enc, C, LM, nbytes,
                      force_intra, &delayedIntra, two_pass, loss_rate, lfe);
  hf(oldBandE, C * nb); hfr(error, C, nb, start, end); hf(&delayedIntra, 1); hi(ec_tell_frac(&enc));

  for (i = 0; i < nb; i++) tf_res[i] = 0;
  for (i = start; i < end; i++) {
    int raw = rnd() & 1, sel = rnd() & 1;
    tf_res[i] = tf_select_table[LM][4 * isTransient + 2 * sel + raw];
  }
  init_caps(m, cap, LM, C);
  for (i = 0; i < nb; i++) {
    int width = C * (m->eBands[i + 1] - m->eBands[i]) << LM;
    int quanta = IMIN(width << BITRES, IMAX(6 << BITRES, width));
    offsets[i] = (rnd() % 6 == 0) ? quanta * (1 + rnd() % 3) : 0;
  }
  bits = (((opus_int32)nbytes * 8) << BITRES) - (opus_int32)ec_tell_frac(&enc) - 1;
  anti_rsv = isTransient && LM >= 2 && bits >= ((LM + 2) << BITRES) ? (1 << BITRES) : 0;
  bits -= anti_rsv;
  codedBands = clt_compute_allocation(m, start, end, offsets, cap, alloc_trim, &intensity, &dual_stereo,
                                      bits, &balance, pulses, fine_quant, fine_priority, C, LM, &enc, 1, prev, signalBandwidth);
  hi(codedBands); hi(intensity); hi(dual_stereo); hi(balance);
  hiv(pulses, nb); hiv(fine_quant, nb); hiv(fine_priority, nb);
  quant_fine_energy(m, start, end, oldBandE, error, NULL, fine_quant, &enc, C);
  hf(oldBandE, C * nb); hfr(error, C, nb, start, end);
  seed2 = seed;
  memset(collapse, 0, sizeof(collapse));
  quant_all_bands(1, m, start, end, X, C == 2 ? X + N : NULL, collapse, bandE, pulses, shortBlocks, spread,
                  dual_stereo, intensity, tf_res, nbytes * (8 << BITRES) - anti_rsv, balance, &enc, LM, codedBands,
                  &seed, complexity, 0, disable_inv);
  hf(X, C * N); hb(collapse, C * nb); hi(seed); hi(ec_tell_frac(&enc));
  if (anti_rsv > 0) { anti_on = rnd() & 1; ec_enc_bits(&enc, anti_on, 1); }
  quant_energy_finalise(m, start, end, oldBandE, error, fine_quant, fine_priority, nbytes * 8 - ec_tell(&enc), &enc, C);
  hf(oldBandE, C * nb); hfr(error, C, nb, start, end);
  ec_enc_done(&enc);
  hb(buf, nbytes); hi(enc.rng); hi(enc.error);

  /* decode it back */
  ec_dec_init(&dec, buf, nbytes);
  {
    int intra = 0;
    if (ec_tell(&dec) + 3 <= nbytes * 8) intra = ec_dec_bit_logp(&dec, 3);
    unquant_coarse_energy(m, start, end, oldBandE2, intra, &dec, C, LM);
  }
  hf(oldBandE2, C * nb);
  bits = (((opus_int32)nbytes * 8) << BITRES) - (opus_int32)ec_tell_frac(&dec) - 1;
  bits -= anti_rsv;
  codedBands2 = clt_compute_allocation(m, start, end, offsets, cap, alloc_trim, &intensity2, &dual2,
                                       bits, &balance2, pulses2, fine_quant2, fine_priority2, C, LM, &dec, 0, 0, 0);
  hi(codedBands2); hi(intensity2); hi(dual2); hi(balance2);
  hiv(pulses2, nb); hiv(fine_quant2, nb); hiv(fine_priority2, nb);
  unquant_fine_energy(m, start, end, oldBandE2, NULL, fine_quant2, &dec, C);
  memset(collapse2, 0, sizeof(collapse2));
  quant_all_bands(0, m, start, end, X2, C == 2 ? X2 + N : NULL, collapse2, NULL, pulses2, shortBlocks, spread,
                  dual2, intensity2, tf_res, nbytes * (8 << BITRES) - anti_rsv, balance2, &dec, LM, codedBands2,
                  &seed2, 0, 0, disable_inv);
  hf(X2, C * N); hb(collapse2, C * nb); hi(seed2);
  anti_on = 0;
  if (anti_rsv > 0) anti_on = ec_dec_bits(&dec, 1);
  unquant_energy_finalise(m, start, end, oldBandE2, fine_quant2, fine_priority2, nbytes * 8 - ec_tell(&dec), &dec, C);
  hf(oldBandE2, C * nb);
  if (anti_on) {
    anti_collapse(m, X2, collapse2, LM, C, N, start, end, oldBandE2, prev1, prev2, pulses2, seed2, 0, 0);
    hf(X2, C * N);
  }
  hi(dec.rng); hi(ec_tell_frac(&dec)); hi(dec.error);
}

int main(int argc, char **argv) {
  const CELTMode *m = opus_custom_mode_create(48000, 960, NULL);
  int i, n;
  const char *dump = getenv("CELT_DUMP");
  if (argc != 3) return 2;
  if (dump) dumpf = fopen(dump, "wb");
  S = (uint32_t)strtoul(argv[1], 0, 10);
  n = atoi(argv[2]);
  H = 1469598103934665603ULL;
  for (i = 0; i < n; i++) one_case(m);
  printf("hash=%016llx\n", (unsigned long long)H);
  return 0;
}

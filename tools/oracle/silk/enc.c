/* SILK encoder golden driver for internal/silk (see testdata/README):
   runs silk_Encode per packet over a control script and PCM written by
   the Go test (TestWriteEncGoldenInputs), then decodes the packets with
   silk_Decode. Prints one line per packet. Build against a libopus 1.6.1
   tree configured with the defaults (no DRED, deep PLC or OSCE) with
   -include <tree>/config.h (see gen.sh). */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <math.h>
#include "opus.h"
#include "entenc.h"
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
#define FNV0 14695981039346656037ULL

int main(int argc, char **argv) {
  if (argc < 5) { fprintf(stderr, "usage: enc pcm script channels out\n"); return 1; }
  FILE *fp = fopen(argv[1], "rb");
  FILE *fs = fopen(argv[2], "r");
  int ch = atoi(argv[3]);
  FILE *fo = fopen(argv[4], "w");
  if (!fp || !fs || !fo) { fprintf(stderr, "open failed\n"); return 1; }
  fseek(fp, 0, SEEK_END); long sz = ftell(fp); fseek(fp, 0, SEEK_SET);
  int16_t *pcm = malloc(sz);
  if (fread(pcm, 1, sz, fp) != (size_t)sz) { fprintf(stderr, "short read\n"); return 1; }
  int esize; silk_Get_Encoder_Size(&esize, ch);
  void *enc = calloc(1, esize);
  silk_EncControlStruct ctl; memset(&ctl, 0, sizeof(ctl));
  silk_InitEncoder(enc, ch, 0, &ctl);
  int dsize; silk_Get_Decoder_Size(&dsize);
  void *dec = calloc(1, dsize);
  silk_InitDecoder(dec);
  silk_DecControlStruct dctl; memset(&dctl, 0, sizeof(dctl));
  uint64_t dh = FNV0; long dn = 0;
  long pos = 0; int idx = 0;
  int v[21];
  static float in[2 * 48 * 60];
  static float out[2 * 48 * 20];
  while (1) {
    int k;
    for (k = 0; k < 21; k++) if (fscanf(fs, "%d", &v[k]) != 1) break;
    if (k < 21) break;
    int prefill = v[0], ns = v[1];
    ctl.nChannelsAPI = v[2]; ctl.nChannelsInternal = v[3]; ctl.API_sampleRate = v[4];
    ctl.maxInternalSampleRate = v[5]; ctl.minInternalSampleRate = v[6]; ctl.desiredInternalSampleRate = v[7];
    ctl.payloadSize_ms = v[8]; ctl.bitRate = v[9]; ctl.packetLossPercentage = v[10]; ctl.complexity = v[11];
    ctl.useInBandFEC = v[12]; ctl.LBRR_coded = v[13]; ctl.useDTX = v[14]; ctl.useCBR = v[15];
    int maxBytes = v[16];
    ctl.toMono = v[17]; ctl.opusCanSwitch = v[18]; ctl.reducedDependency = v[19];
    int activity = v[20];
    ctl.maxBits = maxBytes * 8;
    for (int i = 0; i < ns * ch; i++) in[i] = pcm[pos * ch + i] * (1.0f / 32768.0f);
    pos += ns;
    unsigned char buf[1275]; memset(buf, 0, sizeof(buf));
    ec_enc e; ec_enc_init(&e, buf, maxBytes);
    opus_int32 nBytes = maxBytes;
    int ret = silk_Encode(enc, &ctl, in, ns, &e, &nBytes, prefill, activity);
    int tell = ec_tell(&e);
    uint64_t h = 0;
    if (!prefill) { ec_enc_done(&e); h = fnv(FNV0, buf, maxBytes); }
    fprintf(fo, "%d %d %d %d %d %u %d %d %d %d %d %d %d %d %016llx", idx, prefill, ret, (int)nBytes, tell,
      (unsigned)e.rng, ctl.switchReady, (int)ctl.internalSampleRate, ctl.inWBmodeWithoutVariableLP,
      ctl.stereoWidth_Q14, ctl.signalType, ctl.offset, ctl.allowBandwidthSwitch, ctl.maxBits,
      (unsigned long long)h);
    if (!prefill) {
      dctl.nChannelsAPI = ch; dctl.nChannelsInternal = ctl.nChannelsInternal;
      dctl.API_sampleRate = ctl.API_sampleRate; dctl.internalSampleRate = ctl.internalSampleRate;
      dctl.payloadSize_ms = ctl.payloadSize_ms;
      int nf = ctl.payloadSize_ms > 20 ? ctl.payloadSize_ms / 20 : 1;
      int lost = nBytes <= 0 ? 1 : 0;
      ec_dec d; ec_dec_init(&d, buf, nBytes > 0 ? nBytes : 0);
      for (int f = 0; f < nf; f++) {
        opus_int32 n = 0;
        int dret = silk_Decode(dec, &dctl, lost, f == 0, &d, out, &n, 0);
        if (dret) fprintf(fo, " decret %d", dret);
        for (int i = 0; i < n * ch; i++) {
          int16_t s = (int16_t)lrintf(out[i] * 32768.0f);
          dh = fnv(dh, &s, 2);
        }
        dn += n;
      }
    }
    fprintf(fo, "\n");
    idx++;
  }
  fprintf(fo, "dec %016llx %ld\n", (unsigned long long)dh, dn);
  fclose(fo);
  return 0;
}

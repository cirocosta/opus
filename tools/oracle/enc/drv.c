/* Reference driver for the Go port of the libopus encoder API
   (opus_encoder_test.go). Links the default-configuration libopus.a.
   Usage: drv pcmfile token...
   The pcm file holds int16 interleaved samples with the channel count of
   the case. Tokens: t=enc|ms|sur|proj fs= ch= app= fr= n= api=16|24|f|f2
   mb= streams= coupled= map=a.b.c family= and controls key=v (before the
   first frame) or @F:key=v (before frame F). Every control prints
   "c key value ret"; every frame prints "ret rng [bw indtx] hex". */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "opus.h"
#include "opus_multistream.h"
#include "opus_projection.h"

#define OPUS_SET_FORCE_MODE_REQUEST 11002

typedef struct { int frame; char key[16]; int val; } ctl_t;

static int apply_enc(OpusEncoder *e, const char *k, int v) {
  if (!strcmp(k,"br")) return opus_encoder_ctl(e, OPUS_SET_BITRATE(v));
  if (!strcmp(k,"cx")) return opus_encoder_ctl(e, OPUS_SET_COMPLEXITY(v));
  if (!strcmp(k,"fec")) return opus_encoder_ctl(e, OPUS_SET_INBAND_FEC(v));
  if (!strcmp(k,"loss")) return opus_encoder_ctl(e, OPUS_SET_PACKET_LOSS_PERC(v));
  if (!strcmp(k,"dtx")) return opus_encoder_ctl(e, OPUS_SET_DTX(v));
  if (!strcmp(k,"vbr")) return opus_encoder_ctl(e, OPUS_SET_VBR(v));
  if (!strcmp(k,"cvbr")) return opus_encoder_ctl(e, OPUS_SET_VBR_CONSTRAINT(v));
  if (!strcmp(k,"sig")) return opus_encoder_ctl(e, OPUS_SET_SIGNAL(v));
  if (!strcmp(k,"bw")) return opus_encoder_ctl(e, OPUS_SET_BANDWIDTH(v));
  if (!strcmp(k,"maxbw")) return opus_encoder_ctl(e, OPUS_SET_MAX_BANDWIDTH(v));
  if (!strcmp(k,"fc")) return opus_encoder_ctl(e, OPUS_SET_FORCE_CHANNELS(v));
  if (!strcmp(k,"lsb")) return opus_encoder_ctl(e, OPUS_SET_LSB_DEPTH(v));
  if (!strcmp(k,"fd")) return opus_encoder_ctl(e, OPUS_SET_EXPERT_FRAME_DURATION(v));
  if (!strcmp(k,"pd")) return opus_encoder_ctl(e, OPUS_SET_PREDICTION_DISABLED(v));
  if (!strcmp(k,"pi")) return opus_encoder_ctl(e, OPUS_SET_PHASE_INVERSION_DISABLED(v));
  if (!strcmp(k,"mode")) return opus_encoder_ctl(e, OPUS_SET_FORCE_MODE_REQUEST, (opus_int32)v);
  if (!strcmp(k,"reset")) return opus_encoder_ctl(e, OPUS_RESET_STATE);
  if (!strcmp(k,"setapp")) return opus_encoder_ctl(e, OPUS_SET_APPLICATION(v));
  if (!strcmp(k,"vr")) return opus_encoder_ctl(e, 11018, (opus_int32)v);
  fprintf(stderr, "bad ctl %s\n", k); exit(1);
}

static int apply_ms(OpusMSEncoder *e, const char *k, int v) {
  if (!strcmp(k,"br")) return opus_multistream_encoder_ctl(e, OPUS_SET_BITRATE(v));
  if (!strcmp(k,"cx")) return opus_multistream_encoder_ctl(e, OPUS_SET_COMPLEXITY(v));
  if (!strcmp(k,"fec")) return opus_multistream_encoder_ctl(e, OPUS_SET_INBAND_FEC(v));
  if (!strcmp(k,"loss")) return opus_multistream_encoder_ctl(e, OPUS_SET_PACKET_LOSS_PERC(v));
  if (!strcmp(k,"dtx")) return opus_multistream_encoder_ctl(e, OPUS_SET_DTX(v));
  if (!strcmp(k,"vbr")) return opus_multistream_encoder_ctl(e, OPUS_SET_VBR(v));
  if (!strcmp(k,"cvbr")) return opus_multistream_encoder_ctl(e, OPUS_SET_VBR_CONSTRAINT(v));
  if (!strcmp(k,"sig")) return opus_multistream_encoder_ctl(e, OPUS_SET_SIGNAL(v));
  if (!strcmp(k,"bw")) return opus_multistream_encoder_ctl(e, OPUS_SET_BANDWIDTH(v));
  if (!strcmp(k,"maxbw")) return opus_multistream_encoder_ctl(e, OPUS_SET_MAX_BANDWIDTH(v));
  if (!strcmp(k,"fc")) return opus_multistream_encoder_ctl(e, OPUS_SET_FORCE_CHANNELS(v));
  if (!strcmp(k,"lsb")) return opus_multistream_encoder_ctl(e, OPUS_SET_LSB_DEPTH(v));
  if (!strcmp(k,"fd")) return opus_multistream_encoder_ctl(e, OPUS_SET_EXPERT_FRAME_DURATION(v));
  if (!strcmp(k,"pd")) return opus_multistream_encoder_ctl(e, OPUS_SET_PREDICTION_DISABLED(v));
  if (!strcmp(k,"pi")) return opus_multistream_encoder_ctl(e, OPUS_SET_PHASE_INVERSION_DISABLED(v));
  if (!strcmp(k,"mode")) return opus_multistream_encoder_ctl(e, OPUS_SET_FORCE_MODE_REQUEST, (opus_int32)v);
  if (!strcmp(k,"reset")) return opus_multistream_encoder_ctl(e, OPUS_RESET_STATE);
  if (!strcmp(k,"setapp")) return opus_multistream_encoder_ctl(e, OPUS_SET_APPLICATION(v));
  fprintf(stderr, "bad ms ctl %s\n", k); exit(1);
}

static int apply_proj(OpusProjectionEncoder *e, const char *k, int v) {
  if (!strcmp(k,"br")) return opus_projection_encoder_ctl(e, OPUS_SET_BITRATE(v));
  if (!strcmp(k,"cx")) return opus_projection_encoder_ctl(e, OPUS_SET_COMPLEXITY(v));
  if (!strcmp(k,"vbr")) return opus_projection_encoder_ctl(e, OPUS_SET_VBR(v));
  if (!strcmp(k,"reset")) return opus_projection_encoder_ctl(e, OPUS_RESET_STATE);
  fprintf(stderr, "bad proj ctl %s\n", k); exit(1);
}

static void hex(const unsigned char *b, int n) {
  for (int i = 0; i < n; i++) printf("%02x", b[i]);
}

int main(int argc, char **argv) {
  const char *type = "enc";
  int fs = 48000, ch = 1, app = OPUS_APPLICATION_AUDIO, fr = 960, n = 50, api = 16, mb = 1500;
  int streams = 1, coupled = 0, family = 1, nmap = 0, err = 0;
  unsigned char map[256];
  ctl_t ctls[512]; int nctl = 0;
  for (int i = 2; i < argc; i++) {
    char *a = argv[i]; char *eq = strchr(a, '=');
    if (!eq) { fprintf(stderr, "bad token %s\n", a); return 1; }
    *eq = 0; char *v = eq+1;
    if (a[0] == '@') {
      char *colon = strchr(a, ':');
      ctls[nctl].frame = atoi(a+1);
      strcpy(ctls[nctl].key, colon+1); ctls[nctl].val = atoi(v); nctl++;
    } else if (!strcmp(a,"t")) type = v;
    else if (!strcmp(a,"fs")) fs = atoi(v);
    else if (!strcmp(a,"ch")) ch = atoi(v);
    else if (!strcmp(a,"app")) app = atoi(v);
    else if (!strcmp(a,"fr")) fr = atoi(v);
    else if (!strcmp(a,"n")) n = atoi(v);
    else if (!strcmp(a,"api")) api = !strcmp(v,"f") ? 0 : !strcmp(v,"f2") ? 1 : !strcmp(v,"f3") ? 2 : !strcmp(v,"f4") ? 3 : atoi(v);
    else if (!strcmp(a,"mb")) mb = atoi(v);
    else if (!strcmp(a,"streams")) streams = atoi(v);
    else if (!strcmp(a,"coupled")) coupled = atoi(v);
    else if (!strcmp(a,"family")) family = atoi(v);
    else if (!strcmp(a,"map")) { char *p = v; while (*p) { map[nmap++] = (unsigned char)strtol(p, &p, 10); if (*p=='.') p++; } }
    else if (!strcmp(a,"in")) ;
    else { ctls[nctl].frame = 0; strcpy(ctls[nctl].key, a); ctls[nctl].val = atoi(v); nctl++; }
  }
  FILE *f = fopen(argv[1], "rb");
  if (!f) { perror(argv[1]); return 1; }
  size_t total = (size_t)n*fr*ch;
  short *pcm = calloc(total, sizeof(short));
  if (fread(pcm, sizeof(short), total, f) != total) { fprintf(stderr, "short input\n"); return 1; }
  fclose(f);
  int *p24 = malloc(sizeof(int)*fr*ch); float *pf = malloc(sizeof(float)*fr*ch);
  unsigned char *buf = malloc(mb > 0 ? mb : 1);

  OpusEncoder *enc = NULL; OpusMSEncoder *ms = NULL; OpusProjectionEncoder *pj = NULL;
  if (!strcmp(type,"enc")) {
    enc = opus_encoder_create(fs, ch, app, &err);
  } else if (!strcmp(type,"ms")) {
    ms = opus_multistream_encoder_create(fs, ch, streams, coupled, map, app, &err);
  } else if (!strcmp(type,"sur")) {
    ms = opus_multistream_surround_encoder_create(fs, ch, family, &streams, &coupled, map, app, &err);
    if (ms) { printf("layout %d %d", streams, coupled); for (int i = 0; i < ch; i++) printf(" %d", map[i]); printf("\n"); }
  } else if (!strcmp(type,"proj")) {
    pj = opus_projection_ambisonics_encoder_create(fs, ch, family, &streams, &coupled, app, &err);
    if (pj) {
      opus_int32 sz, gain; unsigned char m[65536];
      opus_projection_encoder_ctl(pj, OPUS_PROJECTION_GET_DEMIXING_MATRIX_SIZE(&sz));
      opus_projection_encoder_ctl(pj, OPUS_PROJECTION_GET_DEMIXING_MATRIX_GAIN(&gain));
      opus_projection_encoder_ctl(pj, OPUS_PROJECTION_GET_DEMIXING_MATRIX(m, sz));
      printf("layout %d %d %d %d ", streams, coupled, sz, gain); hex(m, sz); printf("\n");
    }
  }
  printf("create %d\n", err);
  if (err != OPUS_OK) return 0;
  if (enc) {
    opus_int32 la; opus_encoder_ctl(enc, OPUS_GET_LOOKAHEAD(&la)); printf("lookahead %d\n", la);
  }

  for (int fi = 0; fi < n; fi++) {
    for (int c = 0; c < nctl; c++) {
      if (ctls[c].frame != fi) continue;
      int r = enc ? apply_enc(enc, ctls[c].key, ctls[c].val)
            : ms ? apply_ms(ms, ctls[c].key, ctls[c].val)
                 : apply_proj(pj, ctls[c].key, ctls[c].val);
      printf("c %s %d %d\n", ctls[c].key, ctls[c].val, r);
    }
    short *in = pcm + (size_t)fi*fr*ch;
    int ret;
    if (api == 24) {
      for (int i = 0; i < fr*ch; i++) p24[i] = in[i]*256 + ((i*31 + fi*7) & 255) - 128;
      ret = enc ? opus_encode24(enc, p24, fr, buf, mb) : ms ? opus_multistream_encode24(ms, p24, fr, buf, mb)
                : opus_projection_encode24(pj, p24, fr, buf, mb);
    } else if (api >= 0 && api <= 3) {
      float sc = api == 0 ? 1.f/32768 : api == 1 ? 1.f/16384 : api == 2 ? 1.f/32 : 1.f;
      for (int i = 0; i < fr*ch; i++) pf[i] = in[i]*sc;
      ret = enc ? opus_encode_float(enc, pf, fr, buf, mb) : ms ? opus_multistream_encode_float(ms, pf, fr, buf, mb)
                : opus_projection_encode_float(pj, pf, fr, buf, mb);
    } else {
      ret = enc ? opus_encode(enc, in, fr, buf, mb) : ms ? opus_multistream_encode(ms, in, fr, buf, mb)
                : opus_projection_encode(pj, in, fr, buf, mb);
    }
    opus_uint32 rng = 0;
    if (enc) opus_encoder_ctl(enc, OPUS_GET_FINAL_RANGE(&rng));
    else if (ms) opus_multistream_encoder_ctl(ms, OPUS_GET_FINAL_RANGE(&rng));
    else opus_projection_encoder_ctl(pj, OPUS_GET_FINAL_RANGE(&rng));
    printf("%d %u", ret, rng);
    if (enc) {
      opus_int32 bw, indtx, br;
      opus_encoder_ctl(enc, OPUS_GET_BANDWIDTH(&bw));
      opus_encoder_ctl(enc, OPUS_GET_IN_DTX(&indtx));
      opus_encoder_ctl(enc, OPUS_GET_BITRATE(&br));
      printf(" %d %d %d", bw, indtx, br);
    }
    printf(" ");
    if (ret > 0) hex(buf, ret);
    printf("\n");
  }
  if (enc) {
    opus_int32 g[21]; opus_uint32 rng;
    opus_encoder_ctl(enc, OPUS_GET_APPLICATION(&g[0]));
    opus_encoder_ctl(enc, OPUS_GET_BITRATE(&g[1]));
    opus_encoder_ctl(enc, OPUS_GET_FORCE_CHANNELS(&g[2]));
    opus_encoder_ctl(enc, OPUS_GET_MAX_BANDWIDTH(&g[3]));
    opus_encoder_ctl(enc, OPUS_GET_BANDWIDTH(&g[4]));
    opus_encoder_ctl(enc, OPUS_GET_DTX(&g[5]));
    opus_encoder_ctl(enc, OPUS_GET_COMPLEXITY(&g[6]));
    opus_encoder_ctl(enc, OPUS_GET_INBAND_FEC(&g[7]));
    opus_encoder_ctl(enc, OPUS_GET_PACKET_LOSS_PERC(&g[8]));
    opus_encoder_ctl(enc, OPUS_GET_VBR(&g[9]));
    opus_encoder_ctl(enc, 11019, &g[10]);
    opus_encoder_ctl(enc, OPUS_GET_VBR_CONSTRAINT(&g[11]));
    opus_encoder_ctl(enc, OPUS_GET_SIGNAL(&g[12]));
    opus_encoder_ctl(enc, OPUS_GET_LOOKAHEAD(&g[13]));
    opus_encoder_ctl(enc, OPUS_GET_SAMPLE_RATE(&g[14]));
    opus_encoder_ctl(enc, OPUS_GET_FINAL_RANGE(&rng));
    opus_encoder_ctl(enc, OPUS_GET_LSB_DEPTH(&g[15]));
    opus_encoder_ctl(enc, OPUS_GET_EXPERT_FRAME_DURATION(&g[16]));
    opus_encoder_ctl(enc, OPUS_GET_PREDICTION_DISABLED(&g[17]));
    opus_encoder_ctl(enc, OPUS_GET_PHASE_INVERSION_DISABLED(&g[18]));
    opus_encoder_ctl(enc, OPUS_GET_IN_DTX(&g[19]));
    printf("g");
    for (int i = 0; i < 20; i++) printf(" %d", g[i]);
    printf(" %u\n", rng);
  } else {
    opus_int32 g[20]; opus_uint32 rng;
    OpusMSEncoder *m = ms;
#define MSG(...) (ms ? opus_multistream_encoder_ctl(m, __VA_ARGS__) : opus_projection_encoder_ctl(pj, __VA_ARGS__))
    MSG(OPUS_GET_BITRATE(&g[0]));
    MSG(OPUS_GET_LSB_DEPTH(&g[1]));
    MSG(OPUS_GET_VBR(&g[2]));
    MSG(OPUS_GET_APPLICATION(&g[3]));
    MSG(OPUS_GET_BANDWIDTH(&g[4]));
    MSG(OPUS_GET_COMPLEXITY(&g[5]));
    MSG(OPUS_GET_PACKET_LOSS_PERC(&g[6]));
    MSG(OPUS_GET_DTX(&g[7]));
    MSG(OPUS_GET_VBR_CONSTRAINT(&g[8]));
    MSG(OPUS_GET_SIGNAL(&g[9]));
    MSG(OPUS_GET_LOOKAHEAD(&g[10]));
    MSG(OPUS_GET_SAMPLE_RATE(&g[11]));
    MSG(OPUS_GET_INBAND_FEC(&g[12]));
    MSG(OPUS_GET_FORCE_CHANNELS(&g[13]));
    MSG(OPUS_GET_PREDICTION_DISABLED(&g[14]));
    MSG(OPUS_GET_PHASE_INVERSION_DISABLED(&g[15]));
    MSG(OPUS_GET_EXPERT_FRAME_DURATION(&g[16]));
    MSG(11019, &g[17]);
    MSG(OPUS_GET_FINAL_RANGE(&rng));
    printf("g");
    for (int i = 0; i < 18; i++) printf(" %d", g[i]);
    printf(" %u\n", rng);
  }
  return 0;
}

/* golden generator for internal/entcode. links libopus 1.6.1 celt sources. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "entcode.h"
#include "entenc.h"
#include "entdec.h"
#include "laplace.h"

enum { K_ENC, K_BIN, K_LOGP, K_ICDF, K_ICDF16, K_UINT, K_BITS, K_LAP, K_LAPP0,
       K_SHRINK, K_PATCH };

#define MAXOPS 1024
#define MAXP 16

typedef struct {
  int kind;
  int np;
  long long p[MAXP];
  long long eret, etell, efrac;
  long long dret, dtell, dfrac;
} op_t;

static op_t ops[MAXOPS];
static int nops;
static opus_uint32 rs;

static opus_uint32 rnd(void) {
  rs ^= rs << 13; rs ^= rs >> 17; rs ^= rs << 5;
  return rs;
}

static op_t *add(int kind) {
  op_t *o = &ops[nops++];
  memset(o, 0, sizeof(*o));
  o->kind = kind;
  return o;
}

static void p(op_t *o, long long v) { o->p[o->np++] = v; }

static void gen_table(op_t *o, int ftb, int bits8) {
  int n = 2 + rnd() % 7;
  int i;
  unsigned t = 1u << ftb;
  (void)bits8;
  if ((unsigned)n > t) n = t;
  p(o, rnd() % n); /* s */
  p(o, ftb);
  p(o, n);
  for (i = 0; i < n - 1; i++) {
    unsigned mx = t - 1, mn = n - 1 - i;
    unsigned v = mn + rnd() % (mx - mn + 1);
    p(o, v);
    t = v;
  }
  p(o, 0);
}

static void gen_op(int kind) {
  op_t *o = add(kind);
  switch (kind) {
  case K_ENC: {
    unsigned ft = 2 + rnd() % ((1u << (1 + rnd() % 16)) - 1);
    unsigned fl, fh;
    if (ft > 65536) ft = 65536;
    fl = rnd() % ft;
    fh = fl + 1 + rnd() % (ft - fl);
    p(o, fl); p(o, fh); p(o, ft);
  } break;
  case K_BIN: {
    unsigned bits = 1 + rnd() % 16, ft = 1u << bits;
    unsigned fl = rnd() % ft, fh = fl + 1 + rnd() % (ft - fl);
    p(o, fl); p(o, fh); p(o, bits);
  } break;
  case K_LOGP: {
    unsigned logp = 1 + rnd() % 15;
    p(o, rnd() % 2); p(o, logp);
  } break;
  case K_ICDF: gen_table(o, 1 + rnd() % 8, 1); break;
  case K_ICDF16: gen_table(o, 1 + rnd() % 15, 0); break;
  case K_UINT: {
    int b = 1 + rnd() % 32;
    opus_uint32 ft = b == 32 ? rnd() | 0x80000000u : rnd() >> (32 - b);
    if (rnd() % 8 == 0) ft = 0xFFFFFFFFu;
    if (ft < 2) ft = 2;
    p(o, rnd() % ft); p(o, ft);
  } break;
  case K_BITS: {
    unsigned nb = 1 + rnd() % 25;
    p(o, rnd() & ((1u << nb) - 1)); p(o, nb);
  } break;
  case K_LAP: {
    int v = (int)(rnd() % 41) - 20;
    unsigned fs = (1 + rnd() % 200) << 7;
    int decay = (1 + rnd() % 179) << 6;
    if (rnd() % 8 == 0) v = (rnd() % 2 ? -1 : 1) * (int)(100 + rnd() % 2000);
    p(o, v); p(o, fs); p(o, decay);
  } break;
  case K_LAPP0: {
    int v = (int)(rnd() % 121) - 60;
    if (rnd() % 3 == 0) v = 0;
    p(o, v); p(o, 1 + rnd() % 32766); p(o, rnd() % 32768);
  } break;
  }
}

static void gen_random(int n, int nkinds) {
  int i;
  for (i = 0; i < n; i++) gen_op(rnd() % nkinds);
}

static int has_kind(int kind) {
  int i;
  for (i = 0; i < nops; i++) if (ops[i].kind == kind) return 1;
  return 0;
}

/* shrink sizes of -1 mean: offs+end_offs+extra, resolved at encode time. */
static void run_case(const char *name, int storage, unsigned char *garbage) {
  unsigned char *buf = NULL;
  ec_enc enc;
  ec_dec dec;
  int i, flags = 0, storage0 = storage;
  if (storage > 0) {
    buf = malloc(storage);
    memset(buf, 0xA5, storage);
  }
  if (garbage) {
    memcpy(buf, garbage, storage);
    flags |= 2;
  } else {
    ec_enc_init(&enc, buf, storage);
    for (i = 0; i < nops; i++) {
      op_t *o = &ops[i];
      long long *a = o->p;
      switch (o->kind) {
      case K_ENC: ec_encode(&enc, a[0], a[1], a[2]); break;
      case K_BIN: ec_encode_bin(&enc, a[0], a[1], a[2]); break;
      case K_LOGP: ec_enc_bit_logp(&enc, a[0], a[1]); break;
      case K_ICDF: {
        unsigned char t[16]; int j;
        for (j = 0; j < a[2]; j++) t[j] = a[3 + j];
        ec_enc_icdf(&enc, a[0], t, a[1]);
      } break;
      case K_ICDF16: {
        opus_uint16 t[16]; int j;
        for (j = 0; j < a[2]; j++) t[j] = a[3 + j];
        ec_enc_icdf16(&enc, a[0], t, a[1]);
      } break;
      case K_UINT: ec_enc_uint(&enc, a[0], a[1]); break;
      case K_BITS: ec_enc_bits(&enc, a[0], a[1]); break;
      case K_LAP: {
        int v = a[0];
        ec_laplace_encode(&enc, &v, a[1], a[2]);
        o->eret = v;
      } break;
      case K_LAPP0: ec_laplace_encode_p0(&enc, a[0], a[1], a[2]); break;
      case K_SHRINK:
        if (a[0] < 0) a[0] = enc.offs + enc.end_offs + (-a[0] - 1);
        ec_enc_shrink(&enc, a[0]);
        break;
      case K_PATCH: ec_enc_patch_initial_bits(&enc, a[0], a[1]); break;
      }
      o->etell = ec_tell(&enc);
      o->efrac = ec_tell_frac(&enc);
    }
    ec_enc_done(&enc);
    storage = enc.storage;
    if (!enc.error && !has_kind(K_PATCH)) flags |= 1;
  }
  ec_dec_init(&dec, buf, storage);
  for (i = 0; i < nops; i++) {
    op_t *o = &ops[i];
    long long *a = o->p;
    switch (o->kind) {
    case K_ENC:
      o->dret = ec_decode(&dec, a[2]);
      ec_dec_update(&dec, a[0], a[1], a[2]);
      break;
    case K_BIN:
      o->dret = ec_decode_bin(&dec, a[2]);
      ec_dec_update(&dec, a[0], a[1], 1u << a[2]);
      break;
    case K_LOGP: o->dret = ec_dec_bit_logp(&dec, a[1]); break;
    case K_ICDF: {
      unsigned char t[16]; int j;
      for (j = 0; j < a[2]; j++) t[j] = a[3 + j];
      o->dret = ec_dec_icdf(&dec, t, a[1]);
    } break;
    case K_ICDF16: {
      opus_uint16 t[16]; int j;
      for (j = 0; j < a[2]; j++) t[j] = a[3 + j];
      o->dret = ec_dec_icdf16(&dec, t, a[1]);
    } break;
    case K_UINT: o->dret = ec_dec_uint(&dec, a[1]); break;
    case K_BITS: o->dret = ec_dec_bits(&dec, a[1]); break;
    case K_LAP: o->dret = ec_laplace_decode(&dec, a[1], a[2]); break;
    case K_LAPP0: o->dret = ec_laplace_decode_p0(&dec, a[1], a[2]); break;
    default: break;
    }
    o->dtell = ec_tell(&dec);
    o->dfrac = ec_tell_frac(&dec);
  }
  printf("case %s %d %d\n", name, flags, storage0);
  if (garbage) {
    printf("input ");
    for (i = 0; i < storage; i++) printf("%02x", buf[i]);
    printf("\n");
  }
  for (i = 0; i < nops; i++) {
    op_t *o = &ops[i];
    int j;
    printf("op %d", o->kind);
    for (j = 0; j < o->np; j++) printf(" %lld", o->p[j]);
    printf(" = %lld %lld %lld %lld %lld %lld\n", o->eret, o->etell, o->efrac,
           o->dret, o->dtell, o->dfrac);
  }
  if (!garbage) {
    printf("enc %d %u %u %u %u %u %d %u\n", enc.error, enc.offs, enc.end_offs,
           enc.storage, enc.rng, enc.val, ec_tell(&enc), ec_tell_frac(&enc));
    printf("bytes ");
    for (i = 0; i < storage; i++) printf("%02x", buf[i]);
    printf("-\n");
  }
  printf("dec %d %u %d %u\n", dec.error, dec.rng, ec_tell(&dec),
         ec_tell_frac(&dec));
  printf("end\n");
  free(buf);
  nops = 0;
}

static void logp(int v, int l) { op_t *o = add(K_LOGP); p(o, v); p(o, l); }
static void bits(unsigned v, int n) { op_t *o = add(K_BITS); p(o, v); p(o, n); }
static void patch(unsigned v, int n) { op_t *o = add(K_PATCH); p(o, v); p(o, n); }
static void shrink(long long s) { op_t *o = add(K_SHRINK); p(o, s); }
static void euint(opus_uint32 fl, opus_uint32 ft) { op_t *o = add(K_UINT); p(o, fl); p(o, ft); }
static void ebin(unsigned fl, unsigned fh, int b) { op_t *o = add(K_BIN); p(o, fl); p(o, fh); p(o, b); }

int main(void) {
  char name[64];
  int i, s;
  unsigned char g[256];

  rs = 1; gen_random(200, 9); run_case("mixed1", 1275, NULL);
  rs = 2; gen_random(250, 9); run_case("mixed2", 600, NULL);
  rs = 3;
  for (i = 0; i < 150; i++) gen_op(rnd() % 3 == 0 ? K_UINT : (rnd() % 2 ? K_BITS : K_LOGP));
  run_case("uintbits", 400, NULL);
  rs = 4; gen_random(40, 9); run_case("overflow3", 3, NULL);
  rs = 5; gen_random(60, 9); run_case("overflow8", 8, NULL);
  for (s = 1; s <= 4; s++) {
    for (i = 0; i < 6; i++) {
      int n, k;
      rs = 100 + s * 10 + i;
      n = 1 + rnd() % 8;
      for (k = 0; k < n; k++) {
        int c = rnd() % 3;
        if (c == 0) logp(rnd() % 2, 1 + rnd() % 15);
        else if (c == 1) { unsigned b = 1 + rnd() % 8; ebin(rnd() % (1u << b), 0, b); ops[nops-1].p[1] = ops[nops-1].p[0] + 1; }
        else { unsigned nb = 1 + rnd() % 12; bits(rnd() & ((1u << nb) - 1), nb); }
      }
      snprintf(name, sizeof(name), "tiny_s%d_%d", s, i);
      run_case(name, s, NULL);
    }
  }
  rs = 7; gen_random(80, 9); shrink(-21);
  for (i = 0; i < 6; i++) { logp(rnd() % 2, 3); bits(rnd() & 7, 3); }
  run_case("shrink", 400, NULL);
  rs = 8; gen_random(30, 3); shrink(-1); run_case("shrink_tight", 500, NULL);
  rs = 9; for (i = 0; i < 20; i++) gen_op(K_BITS); shrink(-7); bits(5, 3);
  run_case("shrink_endbits", 300, NULL);
  /* upstream test_unit_entropy patch cases */
  logp(0, 1); logp(0, 1); logp(0, 1); logp(0, 1); logp(0, 2);
  patch(3, 2); patch(0, 5);
  run_case("patch_upstream1", 100, NULL);
  logp(0, 1); logp(0, 1); logp(1, 6); logp(0, 2); patch(0, 2);
  run_case("patch_upstream2", 100, NULL);
  ebin(5, 6, 3); patch(2, 3); run_case("patch_rng", 100, NULL);
  ebin(5, 6, 3); ebin(100, 101, 8); ebin(1, 2, 4); patch(3, 2);
  run_case("patch_rem", 100, NULL);
  rs = 12; gen_random(40, 9); patch(1, 1); patch(0x5a, 8); run_case("patch_offs", 400, NULL);
  patch(0, 5); logp(1, 1); run_case("patch_fail", 100, NULL);
  logp(0, 2); for (i = 0; i < 48; i++) bits(0, 1);
  run_case("rawbits_overfill", 2, NULL);
  for (i = 0; i < 17; i++) bits(0, 1);
  run_case("rawbits_17in2", 2, NULL);
  euint(0, 0xFFFFFFFFu); euint(0xFFFFFFFEu, 0xFFFFFFFFu); euint(0x80000000u, 0xFFFFFFFFu);
  euint(0x7FFFFFFFu, 0x80000000u); euint(0x1000000u, 0x1000001u); euint(255, 256);
  euint(256, 257); euint(1, 2); euint(0, 2); euint(12345, 1u << 24); euint(511, 512);
  euint(0xDEADBEEFu, 0xFFFFFFF0u);
  run_case("uint_large", 200, NULL);
  run_case("empty10", 10, NULL);
  run_case("empty0", 0, NULL);
  logp(1, 1); bits(3, 2); euint(7, 9);
  run_case("ops0", 0, NULL);
  rs = 11;
  for (i = 0; i < 64; i++) g[i] = rnd();
  gen_random(60, 9);
  run_case("garbage", 64, g);
  for (s = 13;; s++) {
    ec_dec d;
    rs = s;
    for (i = 0; i < 160; i++) g[i] = rnd();
    ec_dec_init(&d, g, 160);
    for (i = 0; i < 40; i++) ec_dec_uint(&d, 0x01000001u);
    if (d.error) break;
  }
  for (i = 0; i < 40; i++) euint(0, 0x01000001u);
  run_case("garbage_uint", 160, g);
  return 0;
}
void celt_fatal(const char *str, const char *file, int line) {
  fprintf(stderr, "Fatal (internal) error in %s, line %d: %s\n", file, line, str);
  abort();
}

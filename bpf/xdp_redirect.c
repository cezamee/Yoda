//go:build ignore
// XDP packet redirect and statistics program for AF_XDP
#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>
#include <bpf/bpf_core_read.h>

// Network protocol constants
#define ETH_P_IP 0x0800
#define IPPROTO_TCP 6
#define IPPROTO_UDP 17
#define MAC_SIG 0x3607
#define PORT_TCP_FILTER 443

struct {
    __uint(type, BPF_MAP_TYPE_XSKMAP);
    __uint(key_size, sizeof(__u32));
    __uint(value_size, sizeof(__u32));
    __uint(max_entries, 64);
} xsks_map SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(key_size, sizeof(__u32));
    __uint(value_size, sizeof(__u64));
    __uint(max_entries, 4);
} stats_map SEC(".maps");

#define STATS_TOTAL_PACKETS   0
#define STATS_PORT_TCP        1
#define STATS_PORT_UDP        2
#define STATS_REDIRECTED      3

SEC("xdp")
int xdp_redirect_port(struct xdp_md *ctx) {
    void *data = (void *)(long)ctx->data;
    void *data_end = (void *)(long)ctx->data_end;

    __u32 key = STATS_TOTAL_PACKETS;
    __u64 *counter = bpf_map_lookup_elem(&stats_map, &key);
    if (counter)
        (*counter)++;

    if ((void *)(data + sizeof(struct ethhdr)) > data_end)
        return XDP_PASS;

    struct ethhdr *eth = data;
    __u8 mac0, mac1, mac2, mac3;
    bpf_core_read(&mac0, sizeof(mac0), &eth->h_source[0]);
    bpf_core_read(&mac1, sizeof(mac1), &eth->h_source[1]);
    bpf_core_read(&mac2, sizeof(mac2), &eth->h_source[2]);
    bpf_core_read(&mac3, sizeof(mac3), &eth->h_source[3]);
    __u16 mac_sig = ((mac0 ^ mac2) << 8) | (mac1 ^ mac3);
    if (mac_sig != MAC_SIG)
        return XDP_PASS;

    __u16 h_proto;
    bpf_core_read(&h_proto, sizeof(h_proto), &eth->h_proto);
    if (bpf_ntohs(h_proto) != ETH_P_IP)
        return XDP_PASS;

    if ((void *)(data + sizeof(struct ethhdr) + sizeof(struct iphdr)) > data_end)
        return XDP_PASS;

    struct iphdr *ip = data + sizeof(struct ethhdr);
    __u8 ihl_version;
    bpf_core_read(&ihl_version, sizeof(ihl_version), ip);
    __u8 ip_version = ihl_version >> 4;
    __u8 ihl = ihl_version & 0x0F;
    if (ip_version != 4)
        return XDP_PASS;

    __u8 protocol;
    bpf_core_read(&protocol, sizeof(protocol), &ip->protocol);
    if (protocol != IPPROTO_TCP && protocol != IPPROTO_UDP)
        return XDP_PASS;

    __u32 ip_hdr_len = ihl * 4;
    void *transport_hdr = data + sizeof(struct ethhdr) + ip_hdr_len;

    if (protocol == IPPROTO_TCP) {
        if ((void *)(transport_hdr + 4) > data_end)
            return XDP_PASS;
        __u16 dest_port;
        bpf_core_read(&dest_port, sizeof(dest_port), transport_hdr + 2);
        dest_port = bpf_ntohs(dest_port);
        if (dest_port != PORT_TCP_FILTER)
            return XDP_PASS;
    }

    if (protocol == IPPROTO_TCP) {
        key = STATS_PORT_TCP;
        counter = bpf_map_lookup_elem(&stats_map, &key);
        if (counter)
            (*counter)++;
    } else if (protocol == IPPROTO_UDP) {
        key = STATS_PORT_UDP;
        counter = bpf_map_lookup_elem(&stats_map, &key);
        if (counter)
            (*counter)++;
    }

    __u32 queue_id = 0;
    int ret = bpf_redirect_map(&xsks_map, queue_id, 0);

    if (ret == XDP_REDIRECT) {
        key = STATS_REDIRECTED;
        counter = bpf_map_lookup_elem(&stats_map, &key);
        if (counter)
            (*counter)++;
        return XDP_REDIRECT;
    }

    return XDP_PASS;
}

char _license[] SEC("license") = "GPL";

//go:build ignore
// XDP packet redirect and statistics program for AF_XDP
// Programme XDP pour redirection de paquets et statistiques AF_XDP
#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>
#include <bpf/bpf_core_read.h>

// Network protocol constants
// Constantes réseau
#define ETH_P_IP 0x0800
#define IPPROTO_TCP 6
#define IPPROTO_UDP 17
#define MAC_SIG 0x3607
#define PORT_TCP_FILTER 443

// Map to store AF_XDP socket configuration (CO-RE format)
// Map pour stocker la configuration des sockets AF_XDP (CO-RE format)
struct {
    __uint(type, BPF_MAP_TYPE_XSKMAP);
    __uint(key_size, sizeof(__u32));
    __uint(value_size, sizeof(__u32));
    __uint(max_entries, 64);
} xsks_map SEC(".maps");

// Map to count packets (CO-RE format)
// Map pour compter les paquets (CO-RE format)
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

    // Total packet counter: increment for ALL packets
    // Compteur total de paquets : incrémente pour TOUS les paquets
    __u32 key = STATS_TOTAL_PACKETS;
    __u64 *counter = bpf_map_lookup_elem(&stats_map, &key);
    if (counter)
        (*counter)++;

    // Check minimum size for Ethernet header
    // Vérifie la taille minimale pour l'en-tête Ethernet
    if ((void *)(data + sizeof(struct ethhdr)) > data_end)
        return XDP_PASS;

    // Parse Ethernet header (CO-RE safe)
    // Analyse l'en-tête Ethernet (CO-RE safe)
    struct ethhdr *eth = data;

    // MAC source filtering by XOR signature (low collision, 4 bytes)
    // Filtrage MAC source par signature XOR (faible collision, 4 octets)
    __u8 mac0, mac1, mac2, mac3;
    bpf_core_read(&mac0, sizeof(mac0), &eth->h_source[0]);
    bpf_core_read(&mac1, sizeof(mac1), &eth->h_source[1]);
    bpf_core_read(&mac2, sizeof(mac2), &eth->h_source[2]);
    bpf_core_read(&mac3, sizeof(mac3), &eth->h_source[3]);
    __u16 mac_sig = ((mac0 ^ mac2) << 8) | (mac1 ^ mac3);
    if (mac_sig != MAC_SIG)
        return XDP_PASS;

    // Read Ethernet protocol type
    // Lit le type de protocole Ethernet
    __u16 h_proto;
    bpf_core_read(&h_proto, sizeof(h_proto), &eth->h_proto);
    if (bpf_ntohs(h_proto) != ETH_P_IP)
        return XDP_PASS;

    // Check minimum size for IP header
    // Vérifie la taille minimale pour l'en-tête IP
    if ((void *)(data + sizeof(struct ethhdr) + sizeof(struct iphdr)) > data_end)
        return XDP_PASS;

    // Parse IP header (CO-RE safe, bitfields)
    // Analyse l'en-tête IP (CO-RE safe, bitfields)
    struct iphdr *ip = data + sizeof(struct ethhdr);
    __u8 ihl_version;
    bpf_core_read(&ihl_version, sizeof(ihl_version), ip);
    __u8 ip_version = ihl_version >> 4;
    __u8 ihl = ihl_version & 0x0F;
    if (ip_version != 4)
        return XDP_PASS;

    // Read IP protocol (TCP/UDP)
    // Lit le protocole IP (TCP/UDP)
    __u8 protocol;
    bpf_core_read(&protocol, sizeof(protocol), &ip->protocol);
    if (protocol != IPPROTO_TCP && protocol != IPPROTO_UDP)
        return XDP_PASS;

    __u32 ip_hdr_len = ihl * 4;
    void *transport_hdr = data + sizeof(struct ethhdr) + ip_hdr_len;

    // Filter on TCP port defined by PORT_TCP_FILTER
    // Filtre sur le port TCP défini par PORT_TCP_FILTER
    if (protocol == IPPROTO_TCP) {
        if ((void *)(transport_hdr + 4) > data_end)
            return XDP_PASS;
        __u16 dest_port;
        bpf_core_read(&dest_port, sizeof(dest_port), transport_hdr + 2);
        dest_port = bpf_ntohs(dest_port);
        if (dest_port != PORT_TCP_FILTER)
            return XDP_PASS;
    }

    // Increment statistics by protocol
    // Incrémente les statistiques selon le protocole
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

    // Redirect to AF_XDP
    // Redirige vers AF_XDP
    __u32 queue_id = 0;
    int ret = bpf_redirect_map(&xsks_map, queue_id, 0);

    // If redirected, increment redirected stats
    // Si redirigé, incrémente la stat de redirection
    if (ret == XDP_REDIRECT) {
        key = STATS_REDIRECTED;
        counter = bpf_map_lookup_elem(&stats_map, &key);
        if (counter)
            (*counter)++;
        return XDP_REDIRECT;
    }

    // Default: pass packet
    // Par défaut : laisse passer le paquet
    return XDP_PASS;
}

char _license[] SEC("license") = "GPL";

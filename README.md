

# Yoda: Post-exploitation tool using eBPF xdp / AF_XDP, running on a userspace gVisor network stack

> ⚠️ **Disclaimer / Avertissement**
>
> This project is for research and educational purposes only. The author declines all responsibility for any malicious, illegal, or unethical use of this code. You are solely responsible for how you use, share, or deploy this software. Use it only in controlled, authorized environments and always respect the law.
>
> Ce projet est uniquement destiné à la recherche et à l'apprentissage. L'auteur décline toute responsabilité en cas d'utilisation malveillante, illégale ou contraire à l'éthique de ce code. Vous êtes seul responsable de l'utilisation, du partage ou du déploiement de ce logiciel. Utilisez-le uniquement dans des environnements contrôlés et autorisés, et respectez toujours la loi.


## Description
Yoda is an experimental network server (backdoor ? =P) using AF_XDP, eBPF, and a full userspace network stack (gVisor netstack) to explore low-level packet processing, TLS PTY shell access, and resource management on modern Linux systems. All networking (TCP/IP) is handled outside the Linux kernel, entirely in userspace. The codebase is documented in both English and French for clarity and maintainability. Performance and security are not guaranteed—this project is for research, learning, and demonstration purposes.

## Features
- **AF_XDP Packet I/O:** Lock-free ring buffer, optimized packet processing (best effort, experimental).
- **eBPF/XDP Integration:** Custom XDP programs for MAC-based signature/port filtering and redirection.
- **gVisor Netstack:** User-space TCP/IP stack for isolation, protocol experimentation, and full bypass of the Linux kernel network stack.
- **TLS PTY Shell:** Remote shell access over TLS with dynamic terminal sizing (for demo/testing only).
- **Resource Management:** CPU affinity, buffer pools, and adaptive sleep for efficient resource usage.
- **Bilingual Comments:** All code is documented in both English and French.

## File Overview
- `cmd/cli/main.go` — Yoda client code.
- `cmd/server/main.go` — Entry point, server orchestration.
- `af_xdp.go` — AF_XDP queue management, lock-free ring buffers, packet processing.
- `xdp.go` — XDP program initialization and management.
- `bpf/xdp_redirect.c` — eBPF/XDP C program for packet redirection.
- `netstack.go` — gVisor netstack setup and TCP/TLS server configuration.
- `tls.go` — TLS certificate generation.
- `pty.go` — PTY shell session management over TLS.
- `stats.go` — Real-time statistics and monitoring.
- `config.go` — Configuration constants (network, CPU, etc).
- `utils.go` — Utility functions (CPU affinity, NUMA detection).
- `tools/gen_mac_sig.py` — Python script to generate MAC addresses with custom signatures, or compute the signature of a given MAC address.

## Quick Start
### Prerequisites & Toolchain

You need the following to build and run Yoda:

- **Linux** (kernel 5.4+ recommended)
- **Go 1.20+** (for the main server)
- **Python 3** (for the MAC signature tool)
- **Clang/LLVM** (for compiling the eBPF C program)
- **libbpf** development headers (for eBPF/XDP userspace interaction)
- **bpftool** (or **pahole**) to generate the `vmlinux.h` header for CO-RE eBPF programs
- **Make** (for build automation)
- **Root privileges** (required for AF_XDP, eBPF, loading the XDP program and Yoda itself)


On Ubuntu/Debian, install the required packages with:
```sh
sudo apt-get install clang llvm libbpf-dev bpftool make golang python3 build-essential linux-headers-$(uname -r)
```

- `build-essential` and `linux-headers-$(uname -r)` are needed for compiling C code and eBPF programs against your running kernel.

To generate the `vmlinux.h` header (required for eBPF CO-RE):
```sh
bpftool btf dump file /sys/kernel/btf/vmlinux format c > /same/dir/as/eBPF_prog/vmlinux.h
```
### Generate MAC with Signature
```sh
python3 gen_mac_sig.py 0x4242 10
python3 gen_mac_sig.py --mac aa:bb:cc:dd:ee:ff
```

### Build

Before building, edit `config.go` and `xdp_redirect.c` as needed to match your environment or requirements (e.g., network interface, MAC signature, ports, IP addr).

Build eBPF xdp prog with:
```sh
make bpf
```
Build Yoda
```sh
make yoda
```
Build Yoda cli
```sh
make cli
```
Build all
```sh
make all
```
### Run
```sh
sudo bin/yoda
```

### Test

> ⚠️ **Important:**  
> The client **must** be run from another physical machine on your LAN.  
> Packets must arrive on the actual physical network interface monitored by XDP.  
> Traffic from Docker containers, localhost, or most VMs usually does **not** reach the physical NIC and will **not** trigger the XDP program.  
> For reliable testing, always use a separate physical client machine.

On the client side use yoda cli and enjoy
```sh
./yoda-client <server_addr:port>
```

## Usage
- The server listens for incoming connections and provides a secure PTY shell over TLS.
- All packet processing is handled via AF_XDP queues and custom XDP programs.
- Statistics are printed every 10 seconds.
- Configuration can be adjusted in `config.go`.


## Architecture
```
           +-------------------+
           |      Client       |
           +-------------------+
                     |
                     |  (Internet / LAN)
                     v
           +-------------------+
           |   Network (NIC)   |
           +-------------------+
                     |
                     v
           +-------------------+      +--------------------------+
           |   XDP / eBPF      |--X-->|  Kernel TCP/IP Stack     |
           | (packet filter)   |      |      [BYPASSED] ❌       |
           +-------------------+      +--------------------------+
                     |
                     v
           +-------------------+
           |     AF_XDP        |
           +-------------------+
                     |
                     v
                 USERSpace
           +---------------------------+
           |        Yoda Server        |
           |---------------------------|
           |  gVisor Netstack          |
           |  TLS Layer                |
           |  PTY Shell                |
           +---------------------------+
```

## XDP Filtering & Traffic Camouflage

Yoda uses advanced XDP filtering to select which packets to process:

- **MAC signature filtering (XOR):** The XDP C program (`bpf/xdp_redirect.c`) checks for a weak-collision signature on MAC source addresses (XOR over 4 bytes) and configured port. Only packets with a matching MAC signature / port are accepted; others are passed normally to the linux kernel.
- **Compatible MAC generation:** The Python script `tools/gen_mac_sig.py` generates MAC addresses that match the expected XOR signature for the server or give you the signature of yours.
- **Traffic camouflage:** For example, if Apache is running on port 443, there is no conflict. Yoda does not "bind" the port in the usual sense (it does not use a kernel socket), but receives packets directly from AF_XDP in userspace. Only packets matching Yoda’s XDP filter (MAC/port/signature) are handled by Yoda; all other HTTPS traffic is handled by Apache as usual. This allows Yoda to blend in with legitimate web traffic, enhancing stealth and avoiding detection by standard external monitoring tools.

## Kernel Bypass & Stealth

Yoda uses eBPF xdp filter / AF_XDP and gVisor to fully bypass the Linux kernel network stack:

- **Kernel Bypass:** Packets are processed directly in userspace, never entering the kernel TCP/IP stack.
- **No visible connections:** Connections do not appear in `netstat`, `ss`, or `lsof` because they are not tracked by the kernel.
- **Firewall/tcpdump bypass:** Packets handled by Yoda bypass all firewalls (Netfilter/iptables) and are not visible to tcpdump or other standard monitoring tools on the interface. 
- **Advanced stealth:** Perfect for scenarios requiring maximum network discretion.. =P.

## TODO
- Hide the Yoda process and executable from commands like ps, ls, top, etc., by hooking the getdents*() syscalls.
- Hide XDP mode and XDP program information from appearing in the output of the ip link command.
sendmsg()? write() ?
- ~~Add a custom client for improved functionality.~~

## License
This project is provided under the MIT License. See the header in `bpf/xdp_redirect.c` for eBPF licensing requirements.

## Authors
- Cezame (main developer)
- Contributions welcome!

## Contact
For questions or contributions, open an issue or pull request on the repository.

---


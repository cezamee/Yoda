# Yoda: Post-exploitation Stealth Tool (AF_XDP, eBPF, gVisor)

> [!WARNING]
>
> This project is for research and educational purposes only. The author declines all responsibility for any malicious, illegal, or unethical use of this code. You are solely responsible for how you use, share, or deploy this software. Use it only in controlled, authorized environments and always respect the law.

## 👀 Overview

Yoda is an experimental network server using AF_XDP, eBPF, and a userspace TCP/IP stack (gVisor netstack). It provides stealth remote shell access using websocket, advanced packet filtering, and process hiding. All networking (TCP/IP) is handled outside the Linux kernel, entirely in userspace.

## ✨ Features

- **AF_XDP Packet I/O**
- **eBPF/XDP Integration**
- **gVisor Netstack** 
- **mTLS/Websocket PTY Shell**
- **Process, Binary & Networking Hiding**
- **dmesg & journalctl log output cleaning**
- **ip link output does not reveal XDP program attachment**

## ⚡ Quick Start

### 🗃️ Requirements

You need the following to build Yoda:

- **Linux** (kernel 5.4+ recommended)
- [**Go 1.20+**](https://go.dev/dl/)
- [**Protobuf-compiler**](https://protobuf.dev/installation/)
- [**Python 3**](https://www.python.org/downloads/)
- **Clang/LLVM, libbpf-dev, bpftool, make**
- **Root privileges** (required for AF_XDP, eBPF)

On Ubuntu/Debian, install the required packages:

```sh
$ sudo apt-get install clang llvm libbpf-dev bpftool make golang python3 build-essential linux-headers-$(uname -r)
```

For bpftool, install one of the available packages:

```sh
# Choose one based on your kernel version
$ sudo apt-get install linux-hwe-6.5-tools-common
# or default
$ sudo apt-get install linux-hwe-6.2-tools-common
```

Navigate to the project directory and generate the required eBPF header:

To generate the `vmlinux.h` header (required for eBPF CO-RE):
```sh
$ pwd
/home/cezame/Yoda/
$ bpftool btf dump file /sys/kernel/btf/vmlinux format c > vmlinux.h
```

> [!WARNING]
> WSL2 has limited eBPF support. If the above commands fail:
> Download a compatible vmlinux.h and install the required tools manually.
> ```sh
> $ curl -o vmlinux.h https://raw.githubusercontent.com/libbpf/libbpf/refs/heads/master/.github/actions/build-selftests/vmlinux.h
> $ sudo apt-get install linux-hwe-6.5-tools-common
> ```

### ᝰ.ᐟ MAC Signature Tool

Yoda uses a weak-collision MAC signature to filter packets. The Python script `tools/gen_mac_sig.py` generates compatible MAC addresses or retrieves the signature for a given MAC address.
You can generate a MAC address with a specific signature or retrieve the signature for an existing MAC address.

```sh
$ pwd
/home/cezame/Yoda/tools
$ python3 gen_mac_sig.py 0x4242 10
$ python3 gen_mac_sig.py --mac aa:bb:cc:dd:ee:ff
```

### 🛠️ Build & Run

Before building, edit `internal/config/config.go` and `bpf/xdp_redirect.c` as needed to match your environment or requirements (e.g., network interface, MAC signature, ports, IP addr).

#### ⚙️ Configure ``internal/config/config.go``

##### Network Settings
Before building Yoda, you need to configure the network parameters in ``config/config.go`` to match your environment:

```go
const (
    NetLocalIP    = "192.168.0.38" // Your local IP address
    NetGateway    = "192.168.0.1"  // Your gateway IP
    InterfaceName = "enp46s0"      // Your network interface name
    TcpListenPort = 443            // TCP port for C2 communication
    UdpListenPort = 443            // UDP port for C2 communication
)
```

Finding Your Network Information
1. Get Your Local IP Address
    ```sh
    # Method 1: Using ip command
    $ ip route get 8.8.8.8 | grep -oP 'src \K\S+'
     
    # Method 2: Using hostname command
    $ hostname -I | awk '{print $1}'
     
    # Method 3: Check specific interface
    $ ip addr show | grep 'inet ' | grep -v '127.0.0.1'
    ```
2. Find Your Gateway IP
    ```sh
    # Method 1: Using ip command
    $ ip route | grep default | awk '{print $3}'
    
    # Method 2: Using route command
    $ route -n | grep 'UG[ \t]' | awk '{print $2}'
    ```

3. Identify Your Network Interface
    ```sh
    # List all network interfaces
    $ ip link show

    # Show interfaces with IP addresses
    $ ip addr show

    # Find active interface (excluding loopback)
    $ ip route | grep default | awk '{print $5}'
    ```

> [!WARNING]
> 
> Ensure the configured IP matches your actual network setup
> Wrong interface names will cause AF_XDP socket binding to fail
> The gateway IP must be reachable from your local network

```sh
# First generate mtls certs for cli & yoda
make cert CERT_IP=IP_OF_YODA_SERV # Same ip as netLocalIP in config.go

make bpf        # Build eBPF programs
make yoda       # Build Yoda server
make cli        # Build Yoda client
make all        # Build all
sudo bin/yoda   # Run server
```

### Test

> [!WARNING]  
> The client **must** be run from another physical machine on your LAN.  
> Packets must arrive on the actual physical network interface monitored by XDP.  
> Traffic from Docker containers, localhost, or most VMs usually does **not** reach the physical NIC and will **not** trigger the XDP program.  
> For reliable testing, always use a separate physical client machine.

On the client side use yoda cli and enjoy
```sh
./yoda-client shell
```


---


## 🏗️ Architecture
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
           |  mTLS/ws                  |
           |  PTY Shell                |
           +---------------------------+
```


---


## 🧩 XDP Filter

Yoda uses advanced XDP filtering to select which packets to process:

- **MAC signature filtering (XOR):** The XDP C program (`bpf/xdp_redirect.c`) checks for a weak-collision signature on MAC source addresses (XOR over 4 bytes) and configured port. Only packets with a matching MAC signature / port are accepted; others are passed normally to the linux kernel.
- **Compatible MAC generation:** The Python script `tools/gen_mac_sig.py` generates MAC addresses that match the expected XOR signature for the server or give you the signature of yours.



---

## 🕵️ Stealth & Hiding

- **Kernel Bypass:** Packets are processed directly in userspace, never entering the kernel TCP/IP stack.
- **No visible connections:** No visible connections in `netstat`, `ss`, or `lsof`.
- **Firewall/tcpdump bypass:** Yoda-handled packets bypass Netfilter and conntrack, ignoring iptables rules and remaining invisible to tcpdump and standard network monitors. 
- **Process & Binary Hiding:** Yoda uses an eBPF hook on the `getdents64` syscall to hide its own PIDs, shell PID and binary name from process listings. This means the process and its executable will not appear in `ls`, `ps`, `top`, `htop`, `find` or similar tools, making detection much harder.
- **Files & Directory Hiding:** Yoda can also hide files and directories whose names start with a configured prefix.
- **Traffic camouflage:** Yoda doesn’t bind ports normally but uses AF_XDP to capture only matching packets in userspace. Legitimate traffic (e.g., Apache on port 443) passes through unaffected, letting Yoda blend seamlessly and avoid detection.
- **Log output cleaning:** Kernel warnings and traces related to eBPF actions (e.g., bpf_probe_write_user) are cleaned from `dmesg` and `journalctl` output.
- **Ip link output cleaning:** No XDP program is shown as attached in `ip link` output for the interface.
- **Advanced stealth:** Perfect for scenarios requiring maximum network discretion.. 👻


---

## 📝 TODO
- [ ] **Add extended commands (such as download, upload, etc.)**
- [ ] **Add a mechanism to handle several types of stealth persistence.**
- [ ] **Add uprobe hooks for various TLS/OPENSSL libraries (SSL_READ/WRITE)**
- [ ] **Add uprobe hooks on bash readline() and other shell equivalents**
- [ ] **Add uprobe hooks on pam_get_authtok to sniff PAM logon passwords**
- [x] Hide the Yoda process and executable from commands like ps, ls, top, etc., by hooking the getdents*() syscalls.
- [x] Add a custom client for improved functionality.
- [x] Suppress or hide kernel warnings related to bpf_probe_write_user in dmesg and other system logs.
- [x] Hide XDP program information from appearing in the output of the ip link command. sendmsg()? write() ?

---

## 📄 License
MIT License. See xdp_redirect.c for eBPF license.


---

## 👤 Authors
- Cezame (main developer)
- Contributions welcome!


## 📬 Contact
For questions or contributions, open an issue or pull request on the repository.

---


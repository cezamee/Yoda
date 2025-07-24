#!/usr/bin/env python3
"""
Generates MAC addresses whose 4-byte weak XOR signature matches the desired signature.
Usage: python3 gen_mac_sig.py 0x4242 [count]
"""
import random
import sys

def gen_mac_with_sig(sig, n=5):
    for _ in range(n):
        mac0 = random.randint(0, 127) * 2
        mac1 = random.randint(0, 255)
        s0 = (sig >> 8) & 0xFF
        s1 = sig & 0xFF
        mac2 = mac0 ^ s0
        mac3 = mac1 ^ s1
        mac_rest = [random.randint(0, 255), random.randint(0, 255)]
        mac = [mac0, mac1, mac2, mac3] + mac_rest
        print(':'.join(f"{b:02x}" for b in mac))


def get_mac_sig(mac_str):
    """
    Computes the 4-byte weak XOR signature of a given MAC address.
    Input: mac_str as 'aa:bb:cc:dd:ee:ff'
    Returns: signature (int)
    """
    parts = mac_str.split(":")
    if len(parts) < 4:
        raise ValueError("MAC must have at least 4 octets")
    mac_bytes = [int(b, 16) for b in parts[:4]]
    sig = ((mac_bytes[0] ^ mac_bytes[2]) << 8) | (mac_bytes[1] ^ mac_bytes[3])
    return sig


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: python3 gen_mac_sig.py <signature_hex> [count] | --mac <mac>")
        sys.exit(1)

    if sys.argv[1] == "--mac" and len(sys.argv) > 2:
        mac = sys.argv[2]
        try:
            sig = get_mac_sig(mac)
            print(f"Signature of {mac}: 0x{sig:04x}")
        except Exception as e:
            print(f"Error: {e}")
            sys.exit(1)
    else:
        sig = int(sys.argv[1], 16)
        n = int(sys.argv[2]) if len(sys.argv) > 2 else 5
        gen_mac_with_sig(sig, n)

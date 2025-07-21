// MIT License
// Copyright (c) 2025 Cezame
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

// Network and performance configuration constants
// Constantes de configuration réseau et performance
package main

import (
	"gvisor.dev/gvisor/pkg/tcpip"
)

const (
	// Network interface and IP configuration
	// Configuration de l'interface réseau et IP
	netNicID   = tcpip.NICID(1) // NIC identifier / Identifiant NIC
	netLocalIP = "192.168.0.38" // Local IP address / Adresse IP locale
	netMTU     = 1500           // MTU size / Taille MTU

	// Packet processing parameters
	// Paramètres de traitement des paquets
	ethHeaderSize   = 14        // Ethernet header size / Taille en-tête Ethernet
	ipHeaderMinSize = 20        // Minimum IP header size / Taille minimale en-tête IP
	frameSize       = 2048      // Frame size / Taille de trame
	interfaceName   = "enp46s0" // Network interface name / Nom de l'interface réseau

	ptyBufferSize = 16384 // PTY read buffer (16KB) / Buffer de lecture PTY (16Ko)

	tcpListenPort = 443 // TCP listen port / Port d'écoute TCP

	// CPU affinity for optimal performance
	// Affinité CPU pour performance optimale
	cpuRXProcessing = 0 // RX packet processing core / Cœur RX
	cpuTXProcessing = 1 // TX packet processing core / Cœur TX
	cpuTLSCrypto    = 2 // TLS/crypto operations core / Cœur TLS/crypto
	cpuPTYIO        = 3 // PTY I/O operations core / Cœur PTY I/O
)

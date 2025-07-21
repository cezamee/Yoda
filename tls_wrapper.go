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

/*
TLS wrapper for gVisor endpoints
Enveloppe TLS pour les endpoints gVisor

- Implements net.Conn interface for gVisor endpoints
- Handles non-blocking reads/writes with retry and notification logic
- Used for TLS over custom network stack

- Implémente l'interface net.Conn pour les endpoints gVisor
- Gère les lectures/écritures non bloquantes avec logique de notification et retry
- Utilisé pour TLS sur une stack réseau personnalisée
*/
package main

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/waiter"
)

// gVisorConn wraps a gVisor endpoint to provide net.Conn interface
// gVisorConn encapsule un endpoint gVisor pour fournir l'interface net.Conn
type gVisorConn struct {
	ep     tcpip.Endpoint // gVisor endpoint / Endpoint gVisor
	wq     *waiter.Queue  // Waiter queue for notifications / File d'attente pour notifications
	notify chan struct{}  // Channel for read notifications / Canal de notification lecture
	done   chan struct{}  // Channel for connection close / Canal pour fermeture connexion
}

// Read implements non-blocking read with notification and timeout
// Read implémente une lecture non bloquante avec notification et timeout
func (c *gVisorConn) Read(b []byte) (n int, err error) {
	for {
		var buf bytes.Buffer
		_, tcpErr := c.ep.Read(&buf, tcpip.ReadOptions{})
		if tcpErr == nil {
			data := buf.Bytes()
			n = copy(b, data)
			return n, nil
		}

		if tcpErr.String() == "operation would block" {
			select {
			case <-c.notify:
				continue
			case <-c.done:
				return 0, io.EOF
			case <-time.After(5 * time.Second):
				continue
			}
		} else {
			return 0, fmt.Errorf("gVisor read error: %v", tcpErr)
		}
	}
}

// Write implements non-blocking write with exponential backoff retry
// Write implémente une écriture non bloquante avec retry exponentiel
func (c *gVisorConn) Write(b []byte) (n int, err error) {
	for retries := 0; retries < 10; retries++ {
		_, tcpErr := c.ep.Write(bytes.NewReader(b), tcpip.WriteOptions{})
		if tcpErr == nil {
			return len(b), nil
		}

		if tcpErr.String() == "operation would block" {
			// Exponential backoff / Attente exponentielle
			time.Sleep(time.Duration(1<<uint(retries)) * time.Millisecond)
			continue
		} else {
			return 0, fmt.Errorf("gVisor write error: %v", tcpErr)
		}
	}
	return 0, fmt.Errorf("write failed after retries")
}

// Close closes the connection and underlying endpoint
// Close ferme la connexion et l'endpoint sous-jacent
func (c *gVisorConn) Close() error {
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	c.ep.Close()
	return nil
}

// LocalAddr returns the local address
// LocalAddr retourne l'adresse locale
func (c *gVisorConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP(netLocalIP), Port: tcpListenPort}
}

// Stub for net.Conn interface
func (c *gVisorConn) RemoteAddr() net.Addr               { return nil }
func (c *gVisorConn) SetDeadline(t time.Time) error      { return nil }
func (c *gVisorConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *gVisorConn) SetWriteDeadline(t time.Time) error { return nil }

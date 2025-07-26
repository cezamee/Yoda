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
package core

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
	ep   tcpip.Endpoint // gVisor endpoint / Endpoint gVisor
	wq   *waiter.Queue  // Waiter queue for notifications / File d'attente pour notifications
	done chan struct{}  // Channel for connection close / Canal pour fermeture connexion
}

// Read implements non-blocking read with notification and timeout
// Read implémente une lecture non bloquante avec notification et timeout
func (c *gVisorConn) Read(b []byte) (n int, err error) {
	waitEntry, readable := waiter.NewChannelEntry(waiter.ReadableEvents)
	c.wq.EventRegister(&waitEntry)
	defer c.wq.EventUnregister(&waitEntry)

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
			case <-readable:
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

func (c *gVisorConn) Write(b []byte) (n int, err error) {
	waitEntry, writable := waiter.NewChannelEntry(waiter.WritableEvents)
	c.wq.EventRegister(&waitEntry)
	defer c.wq.EventUnregister(&waitEntry)

	for retries := 0; retries < 10; retries++ {
		_, tcpErr := c.ep.Write(bytes.NewReader(b), tcpip.WriteOptions{})
		if tcpErr == nil {
			return len(b), nil
		}

		if tcpErr.String() == "operation would block" {
			select {
			case <-writable:
				continue
			case <-c.done:
				return 0, io.EOF
			case <-time.After(5 * time.Second):
				continue
			}
		} else {
			return 0, fmt.Errorf("gVisor write error: %v", tcpErr)
		}
	}
	return 0, fmt.Errorf("write failed after retries")
}

func (c *gVisorConn) Close() error {
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	c.ep.Close()
	return nil
}

func (c *gVisorConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP(netLocalIP), Port: tcpListenPort}
}

// Stub for net.Conn interface
func (c *gVisorConn) RemoteAddr() net.Addr               { return nil }
func (c *gVisorConn) SetDeadline(t time.Time) error      { return nil }
func (c *gVisorConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *gVisorConn) SetWriteDeadline(t time.Time) error { return nil }

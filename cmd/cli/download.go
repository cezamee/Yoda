// File download client: handles file download from server via WebSocket with binary protocol

package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Download message types (must match server)
const (
	DownloadMsgTypeRequest  = "download_request"
	DownloadMsgTypeResponse = "download_response"
	DownloadMsgTypeChunk    = "download_chunk"
	DownloadMsgTypeComplete = "download_complete"
	DownloadMsgTypeError    = "download_error"
)

// Binary message types (must match server)
const (
	BinaryMsgTypeChunk    = 0x01
	BinaryMsgTypeComplete = 0x02
	BinaryMsgTypeError    = 0x03
	BinaryMsgTypeAck      = 0x04
)

// Timeout configuration
const (
	DefaultReadTimeout = 30 * time.Second // Timeout per chunk
	HandshakeTimeout   = 15 * time.Second // Timeout for handshake
	WriteTimeout       = 5 * time.Second  // Timeout for sending ACK
)

// Progress bar configuration
const (
	ProgressBarWidth = 30
)

// Buffer pool for reusing ACK message buffers
var ackBufferPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 5)
		return &buf
	},
}

// Download message structure (must match server)
type DownloadMessage struct {
	Type     string `json:"type"`
	FilePath string `json:"file_path,omitempty"`
	FileSize int64  `json:"file_size,omitempty"`
	ChunkID  int    `json:"chunk_id,omitempty"`
	Data     []byte `json:"data,omitempty"`
	Error    string `json:"error,omitempty"`
	Success  bool   `json:"success,omitempty"`
}

// DownloadSession holds the state for a download session
type DownloadSession struct {
	conn          *websocket.Conn
	file          *os.File
	remotePath    string
	localPath     string
	fileSize      int64
	receivedBytes int64
	expectedChunk uint32
	startTime     time.Time
	lastProgress  time.Time
}

// DownloadFile downloads a file from the server via WebSocket
func DownloadFile(conn *websocket.Conn, remotePath, localPath string) error {
	session, err := newDownloadSession(conn, remotePath, localPath)
	if err != nil {
		return err
	}
	defer session.close()

	if err := session.sendRequest(); err != nil {
		return err
	}

	if err := session.handleHandshake(); err != nil {
		return err
	}

	return session.handleDataTransfer()
}

// newDownloadSession creates a new download session
func newDownloadSession(conn *websocket.Conn, remotePath, localPath string) (*DownloadSession, error) {
	fmt.Printf("🔽 Initiating download: %s -> %s\n", remotePath, localPath)

	localDir := filepath.Dir(localPath)
	if err := os.MkdirAll(localDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create local directory: %v", err)
	}

	file, err := os.Create(localPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create local file: %v", err)
	}

	return &DownloadSession{
		conn:       conn,
		file:       file,
		remotePath: remotePath,
		localPath:  localPath,
	}, nil
}

// close cleans up the download session
func (s *DownloadSession) close() {
	if s.file != nil {
		s.file.Close()
	}
}

// sendRequest sends the download request to the server
func (s *DownloadSession) sendRequest() error {
	requestMsg := DownloadMessage{
		Type:     DownloadMsgTypeRequest,
		FilePath: s.remotePath,
	}

	msgBytes, err := json.Marshal(requestMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %v", err)
	}

	return s.conn.WriteMessage(websocket.TextMessage, msgBytes)
}

// handleHandshake handles the JSON handshake phase
func (s *DownloadSession) handleHandshake() error {
	fmt.Printf("📡 Waiting for download response...\n")

	for {
		s.conn.SetReadDeadline(time.Now().Add(HandshakeTimeout))

		messageType, msgBytes, err := s.conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("failed to read handshake message: %v", err)
		}

		if messageType != websocket.TextMessage {
			continue
		}

		var msg DownloadMessage
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			fmt.Printf("⚠️ Failed to unmarshal handshake message: %v\n", err)
			continue
		}

		switch msg.Type {
		case DownloadMsgTypeResponse:
			if !msg.Success {
				return fmt.Errorf("server rejected download request")
			}
			s.fileSize = msg.FileSize
			fmt.Printf("📤 Server accepted download: %s (%d bytes)\n", msg.FilePath, s.fileSize)
			return nil

		case DownloadMsgTypeError:
			return fmt.Errorf("server error: %s", msg.Error)

		default:
			fmt.Printf("⚠️ Unexpected handshake message type: %s\n", msg.Type)
		}
	}
}

// handleDataTransfer handles the binary data transfer phase
func (s *DownloadSession) handleDataTransfer() error {
	s.startTime = time.Now()
	s.lastProgress = time.Now()
	fmt.Printf("📥 Starting binary data reception...\n")

	for {
		s.conn.SetReadDeadline(time.Now().Add(DefaultReadTimeout))

		messageType, msgBytes, err := s.conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("failed to read binary message: %v", err)
		}

		if messageType != websocket.BinaryMessage || len(msgBytes) < 1 {
			continue
		}
		msgType := msgBytes[0]
		switch msgType {
		case BinaryMsgTypeChunk:
			if err := s.processChunk(msgBytes); err != nil {
				return err
			}

		case BinaryMsgTypeComplete:
			s.showCompletionStats()
			return nil

		case BinaryMsgTypeError:
			return s.processBinaryError(msgBytes)

		default:
			fmt.Printf("⚠️ Unknown binary message type: 0x%02x\n", msgType)
		}
	}
}

// processChunk processes a binary chunk message
func (s *DownloadSession) processChunk(msgBytes []byte) error {
	if len(msgBytes) < 9 {
		fmt.Printf("⚠️ Invalid binary chunk format\n")
		return nil
	}

	chunkID := binary.LittleEndian.Uint32(msgBytes[1:5])
	dataLen := binary.LittleEndian.Uint32(msgBytes[5:9])
	data := msgBytes[9:]

	if chunkID != s.expectedChunk {
		fmt.Printf("\n⚠️ Out-of-order chunk! Expected %d, got %d\n", s.expectedChunk, chunkID)
	}
	s.expectedChunk = chunkID + 1

	if len(data) != int(dataLen) {
		fmt.Printf("⚠️ Data length mismatch: expected %d, got %d\n", dataLen, len(data))
		return nil
	}

	n, err := s.file.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write chunk %d: %v", chunkID, err)
	}

	s.receivedBytes += int64(n)

	// Send ACK
	if err := s.sendAck(chunkID); err != nil {
		return fmt.Errorf("failed to send ACK for chunk %d: %v", chunkID, err)
	}

	// Update progress
	s.updateProgress(chunkID)
	return nil
}

// sendAck sends an ACK message for a chunk using buffer pool
func (s *DownloadSession) sendAck(chunkID uint32) error {
	s.conn.SetWriteDeadline(time.Now().Add(WriteTimeout))

	msgPtr := ackBufferPool.Get().(*[]byte)
	defer ackBufferPool.Put(msgPtr)

	msg := *msgPtr
	msg[0] = BinaryMsgTypeAck
	binary.LittleEndian.PutUint32(msg[1:5], chunkID)
	return s.conn.WriteMessage(websocket.BinaryMessage, msg)
}

// updateProgress updates and displays download progress with visual bar and ETA
func (s *DownloadSession) updateProgress(chunkID uint32) {
	now := time.Now()
	if now.Sub(s.lastProgress) < time.Second && chunkID%20 != 0 {
		return
	}

	if s.fileSize > 0 {
		progress := float64(s.receivedBytes) / float64(s.fileSize) * 100
		speed := float64(s.receivedBytes) / now.Sub(s.startTime).Seconds()

		filled := int(progress * float64(ProgressBarWidth) / 100)
		if filled > ProgressBarWidth {
			filled = ProgressBarWidth
		}
		bar := strings.Repeat("#", filled) + strings.Repeat(".", ProgressBarWidth-filled)

		var etaStr string
		if speed > 0 {
			remainingBytes := s.fileSize - s.receivedBytes
			eta := time.Duration(float64(remainingBytes)/speed) * time.Second

			if eta > time.Hour {
				etaStr = fmt.Sprintf("ETA: %dh%dm  ", int(eta.Hours()), int(eta.Minutes())%60)
			} else if eta > time.Minute {
				etaStr = fmt.Sprintf("ETA: %dm%ds  ", int(eta.Minutes()), int(eta.Seconds())%60)
			} else {
				etaStr = fmt.Sprintf("ETA: %ds  ", int(eta.Seconds()))
			}
		} else {
			etaStr = "ETA: --"
		}

		var speedStr string
		if speed > 1024*1024 {
			speedStr = fmt.Sprintf("%.1f MB/s", speed/1024/1024)
		} else {
			speedStr = fmt.Sprintf("%.1f KB/s", speed/1024)
		}

		fmt.Printf("\r📥 [%s] %.1f%% (%s) %s",
			bar, progress, speedStr, etaStr)
	} else {
		fmt.Printf("\r📥 Received: %d bytes (chunk %d)", s.receivedBytes, chunkID)
	}
	s.lastProgress = now
}

// showCompletionStats shows final download statistics
func (s *DownloadSession) showCompletionStats() {
	duration := time.Since(s.startTime)
	avgSpeed := float64(s.receivedBytes) / duration.Seconds()
	fmt.Printf("\r✅ Binary download completed successfully!            \n")
	fmt.Printf("📊 Total: %d bytes in %v (%.1f KB/s)\n",
		s.receivedBytes, duration.Truncate(time.Millisecond), avgSpeed/1024)
	fmt.Printf("💾 File saved to: %s\n", s.localPath)
}

// processBinaryError processes a binary error message
func (s *DownloadSession) processBinaryError(msgBytes []byte) error {
	if len(msgBytes) < 5 {
		return fmt.Errorf("binary error message too short")
	}

	errorLen := binary.LittleEndian.Uint32(msgBytes[1:5])
	if len(msgBytes) < int(5+errorLen) {
		return fmt.Errorf("binary error message truncated")
	}

	errorMsg := string(msgBytes[5 : 5+errorLen])
	return fmt.Errorf("server binary error: %s", errorMsg)
}

// downloadCommand handles the download command
func downloadCommand(conn *websocket.Conn, args []string) {
	remotePath := args[0]
	localPath := args[1]
	if _, err := os.Stat(localPath); err == nil {
		fmt.Printf("⚠️ Local file '%s' already exists. Overwrite? (y/N): ", localPath)
		var response string
		fmt.Scanln(&response)
		if response != "y" && response != "Y" && response != "yes" {
			fmt.Printf("❌ Download cancelled\n")
			return
		}
	}
	if err := DownloadFile(conn, remotePath, localPath); err != nil {
		fmt.Printf("❌ Download failed: %v\n", err)
	}

	conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
}

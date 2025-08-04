// File download service: handles secure file transfer from server to client over WebSocket

package services

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/gorilla/websocket"
)

// Download message types
const (
	DownloadMsgTypeRequest  = "download_request"
	DownloadMsgTypeResponse = "download_response"
	DownloadMsgTypeChunk    = "download_chunk"
	DownloadMsgTypeComplete = "download_complete"
	DownloadMsgTypeError    = "download_error"
)

// Binary message types
const (
	BinaryMsgTypeChunk    = 0x01
	BinaryMsgTypeComplete = 0x02
	BinaryMsgTypeError    = 0x03
	BinaryMsgTypeAck      = 0x04
)

// Download message structure
type DownloadMessage struct {
	Type     string `json:"type"`
	FilePath string `json:"file_path,omitempty"`
	FileSize int64  `json:"file_size,omitempty"`
	ChunkID  int    `json:"chunk_id,omitempty"`
	Data     []byte `json:"data,omitempty"`
	Error    string `json:"error,omitempty"`
	Success  bool   `json:"success,omitempty"`
}

const (
	MaxChunkSize = 80 * 1024
	WindowSize   = 3
)

// HandleWebSocketDownload processes file download requests over WebSocket
func HandleWebSocketDownload(conn *websocket.Conn) {
	fmt.Printf("🔽 Download service started\n")

	defer func() {
		fmt.Printf("🧹 Download service connection closed\n")
		conn.Close()
	}()

	for {
		_, msgBytes, err := conn.ReadMessage()
		if err != nil {
			fmt.Printf("❌ Failed to read WebSocket message: %v\n", err)
			return
		}

		var msg DownloadMessage
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			fmt.Printf("❌ Failed to unmarshal download message: %v\n", err)
			sendErrorMessage(conn, "Invalid message format")
			continue
		}

		switch msg.Type {
		case DownloadMsgTypeRequest:
			handleDownloadRequest(conn, msg.FilePath)
		default:
			fmt.Printf("⚠️ Unknown download message type: %s\n", msg.Type)
			sendErrorMessage(conn, "Unknown message type")
		}
	}
}

// handleDownloadRequest processes a file download request
func handleDownloadRequest(conn *websocket.Conn, filePath string) {
	fmt.Printf("🔽 Download request for: %s\n", filePath)

	cleanPath := filePath

	fileInfo, err := os.Stat(cleanPath)
	if err != nil {
		fmt.Printf("❌ File not found: %s - %v\n", cleanPath, err)
		sendErrorMessage(conn, fmt.Sprintf("File not found: %s", cleanPath))
		return
	}

	if fileInfo.IsDir() {
		fmt.Printf("🚫 Attempted to download directory: %s\n", cleanPath)
		sendErrorMessage(conn, "Cannot download directories")
		return
	}

	fmt.Printf("� File info: %s (%d bytes)\n", cleanPath, fileInfo.Size())

	responseMsg := DownloadMessage{
		Type:     DownloadMsgTypeResponse,
		FilePath: cleanPath,
		FileSize: fileInfo.Size(),
		Success:  true,
	}

	if err := sendDownloadMessage(conn, responseMsg); err != nil {
		fmt.Printf("❌ Failed to send download response: %v\n", err)
		return
	}

	file, err := os.Open(cleanPath)
	if err != nil {
		fmt.Printf("❌ Failed to open file %s: %v\n", cleanPath, err)
		sendErrorMessage(conn, fmt.Sprintf("Failed to open file: %v", err))
		return
	}
	defer file.Close()

	buffer := make([]byte, MaxChunkSize)
	chunkID := 0
	totalSent := int64(0)
	inFlight := make(map[uint32]bool)

	fmt.Printf("📤 Starting binary transfer of %s (%d bytes) with window size %d\n", cleanPath, fileInfo.Size(), WindowSize)

	for {
		// Send chunks up to window size
		for len(inFlight) < WindowSize {
			n, err := file.Read(buffer)
			if err != nil && err != io.EOF {
				fmt.Printf("❌ Failed to read file chunk: %v\n", err)
				sendBinaryError(conn, fmt.Sprintf("Failed to read file: %v", err))
				return
			}

			if n == 0 {
				break
			}

			if err := sendBinaryChunk(conn, uint32(chunkID), buffer[:n]); err != nil {
				fmt.Printf("❌ Failed to send binary chunk %d: %v\n", chunkID, err)
				return
			}

			inFlight[uint32(chunkID)] = true
			totalSent += int64(n)
			chunkID++
		}

		if len(inFlight) == 0 {
			break
		}

		if err := waitForSlidingAck(conn, inFlight); err != nil {
			fmt.Printf("❌ Failed to receive sliding ACK: %v\n", err)
			return
		}
	}

	for len(inFlight) > 0 {
		if err := waitForSlidingAck(conn, inFlight); err != nil {
			fmt.Printf("❌ Failed to receive final ACK: %v\n", err)
			return
		}
	}

	if err := sendBinaryComplete(conn); err != nil {
		fmt.Printf("❌ Failed to send binary completion: %v\n", err)
		return
	}

	fmt.Printf("✅ Binary download completed: %s (%d bytes, %d chunks)\n", cleanPath, totalSent, chunkID)
}

// sendDownloadMessage sends a download message over WebSocket
func sendDownloadMessage(conn *websocket.Conn, msg DownloadMessage) error {
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %v", err)
	}

	return conn.WriteMessage(websocket.TextMessage, msgBytes)
}

// sendBinaryChunk sends a binary chunk
func sendBinaryChunk(conn *websocket.Conn, chunkID uint32, data []byte) error {
	header := make([]byte, 9)
	header[0] = BinaryMsgTypeChunk
	binary.LittleEndian.PutUint32(header[1:5], chunkID)
	binary.LittleEndian.PutUint32(header[5:9], uint32(len(data)))

	msg := append(header, data...)
	return conn.WriteMessage(websocket.BinaryMessage, msg)
}

// sendBinaryComplete sends a binary completion message
func sendBinaryComplete(conn *websocket.Conn) error {
	msg := []byte{BinaryMsgTypeComplete}
	return conn.WriteMessage(websocket.BinaryMessage, msg)
}

// sendBinaryError sends a binary error message
func sendBinaryError(conn *websocket.Conn, errorMsg string) error {
	errorBytes := []byte(errorMsg)
	header := make([]byte, 5)
	header[0] = BinaryMsgTypeError
	binary.LittleEndian.PutUint32(header[1:5], uint32(len(errorBytes)))

	msg := append(header, errorBytes...)
	return conn.WriteMessage(websocket.BinaryMessage, msg)
}

// sendErrorMessage sends an error message to the client
func sendErrorMessage(conn *websocket.Conn, errorMsg string) {
	errorResponse := DownloadMessage{
		Type:    DownloadMsgTypeError,
		Error:   errorMsg,
		Success: false,
	}

	if err := sendDownloadMessage(conn, errorResponse); err != nil {
		fmt.Printf("❌ Failed to send error message: %v\n", err)
	}
}

// waitForSlidingAck waits for any ACK message and removes it from inFlight map
func waitForSlidingAck(conn *websocket.Conn, inFlight map[uint32]bool) error {
	_, msgBytes, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("failed to read sliding ACK message: %v", err)
	}

	if len(msgBytes) < 5 {
		return fmt.Errorf("sliding ACK message too short: %d bytes", len(msgBytes))
	}

	msgType := msgBytes[0]
	if msgType != BinaryMsgTypeAck {
		return fmt.Errorf("expected sliding ACK message type %d, got %d", BinaryMsgTypeAck, msgType)
	}

	chunkID := binary.LittleEndian.Uint32(msgBytes[1:5])

	if _, exists := inFlight[chunkID]; exists {
		delete(inFlight, chunkID)
	} else {
		fmt.Printf("⚠️ Received ACK for unexpected chunk %d\n", chunkID)
	}

	return nil
}

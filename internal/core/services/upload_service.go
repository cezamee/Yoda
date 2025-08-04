// Native Go file upload service: provides upload functionality with chunked binary transfer over WebSocket
package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/websocket"
)

// UploadMessage structure for WebSocket communication
type UploadMessage struct {
	Type        string `json:"type"`
	Command     string `json:"command,omitempty"`
	Output      string `json:"output,omitempty"`
	Error       string `json:"error,omitempty"`
	Filename    string `json:"filename,omitempty"`
	RemotePath  string `json:"remote_path,omitempty"`
	FileSize    int64  `json:"file_size,omitempty"`
	ChunkIndex  int    `json:"chunk_index,omitempty"`
	TotalChunks int    `json:"total_chunks,omitempty"`
	ChunkSize   int    `json:"chunk_size,omitempty"`
	Uploaded    bool   `json:"uploaded,omitempty"`
	IsComplete  bool   `json:"is_complete,omitempty"`
}

// Active upload sessions to handle chunked uploads
var activeUploads = make(map[string]*UploadSession)

type UploadSession struct {
	Filename     string
	RemotePath   string
	FileSize     int64
	TotalChunks  int
	ReceivedData []byte
	File         *os.File
}

// HandleWebSocketUploadSession handles upload commands over WebSocket
func HandleWebSocketUploadSession(conn *websocket.Conn) {
	fmt.Printf("📤 Starting Upload service session\n")

	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("🚨 Upload service panic: %v\n", r)
		}
		fmt.Printf("🧹 Cleaning up Upload service session...\n")
		conn.Close()
	}()

	for {
		msgType, msgBytes, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				fmt.Printf("📡 WebSocket closed normally: %v\n", err)
			} else if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				fmt.Printf("📡 WebSocket unexpected close: %v\n", err)
			} else {
				fmt.Printf("📡 WebSocket closed: %v\n", err)
			}
			return
		}

		if msgType == websocket.CloseMessage {
			fmt.Printf("📡 Received close message from client\n")
			return
		}

		if msgType == websocket.BinaryMessage {
			handleUploadChunk(conn, msgBytes)
			continue
		}

		if msgType == websocket.TextMessage {
			var msg UploadMessage
			if err := json.Unmarshal(msgBytes, &msg); err != nil {
				sendUploadError(conn, "Invalid JSON message")
				continue
			}

			switch msg.Type {
			case "upload_start":
				handleUploadStart(conn, &msg)
			case "upload_confirm_ok":
				if msg.RemotePath != "" {
					os.Remove(msg.RemotePath)
				}
				prevSession, exists := activeUploads[msg.RemotePath]
				if exists {
					if msg.Filename == "" {
						msg.Filename = prevSession.Filename
					}
					if msg.FileSize == 0 {
						msg.FileSize = prevSession.FileSize
					}
					if msg.TotalChunks == 0 {
						msg.TotalChunks = prevSession.TotalChunks
					}
				}
				handleUploadStart(conn, &msg)
			case "upload_confirm_cancel":
				sendUploadError(conn, "Upload cancelled by client")
			case "upload_complete":
				handleUploadComplete(conn, &msg)
			default:
				sendUploadError(conn, "Unknown message type: "+msg.Type)
			}
		}
	}
}

// handleUploadStart initializes a new upload session
func handleUploadStart(conn *websocket.Conn, msg *UploadMessage) {
	if msg.RemotePath == "" || msg.Filename == "" {
		sendUploadError(conn, "Remote path and filename are required")
		return
	}

	if _, err := os.Stat(msg.RemotePath); err == nil {
		confirmMsg := UploadMessage{
			Type:       "upload_confirm",
			Filename:   msg.Filename,
			RemotePath: msg.RemotePath,
			FileSize:   msg.FileSize,
		}
		sendUploadResponse(conn, &confirmMsg)
		return
	}

	dir := filepath.Dir(msg.RemotePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		sendUploadError(conn, fmt.Sprintf("Failed to create directory '%s': %v", dir, err))
		return
	}

	file, err := os.Create(msg.RemotePath)
	if err != nil {
		sendUploadError(conn, fmt.Sprintf("Failed to create file '%s': %v", msg.RemotePath, err))
		return
	}

	sessionID := msg.RemotePath
	activeUploads[sessionID] = &UploadSession{
		Filename:    msg.Filename,
		RemotePath:  msg.RemotePath,
		FileSize:    msg.FileSize,
		TotalChunks: msg.TotalChunks,
		File:        file,
	}

	fmt.Printf("📤 Starting upload: %s (%d bytes, %d chunks)\n", msg.Filename, msg.FileSize, msg.TotalChunks)

	response := UploadMessage{
		Type:    "upload_ready",
		Command: fmt.Sprintf("upload %s %s", msg.Filename, msg.RemotePath),
	}

	sendUploadResponse(conn, &response)
}

// handleUploadChunk processes incoming file chunks
func handleUploadChunk(conn *websocket.Conn, msgBytes []byte) {
	var headerLen int32
	if len(msgBytes) < 4 {
		sendUploadError(conn, "Invalid chunk message format")
		return
	}

	headerLen = int32(msgBytes[0]) | int32(msgBytes[1])<<8 | int32(msgBytes[2])<<16 | int32(msgBytes[3])<<24

	if int(headerLen) >= len(msgBytes) {
		sendUploadError(conn, "Invalid header length")
		return
	}

	var msg UploadMessage
	if err := json.Unmarshal(msgBytes[4:4+headerLen], &msg); err != nil {
		sendUploadError(conn, "Invalid chunk header")
		return
	}

	chunkData := msgBytes[4+headerLen:]

	session, exists := activeUploads[msg.RemotePath]
	if !exists {
		sendUploadError(conn, "Upload session not found")
		return
	}

	if _, err := session.File.Write(chunkData); err != nil {
		sendUploadError(conn, fmt.Sprintf("Failed to write chunk: %v", err))
		return
	}

	fmt.Printf("📤 Received chunk %d/%d for %s (%d bytes)\n",
		msg.ChunkIndex+1, msg.TotalChunks, msg.Filename, len(chunkData))

	response := UploadMessage{
		Type:       "chunk_ack",
		ChunkIndex: msg.ChunkIndex,
	}

	sendUploadResponse(conn, &response)
}

// handleUploadComplete finalizes the upload
func handleUploadComplete(conn *websocket.Conn, msg *UploadMessage) {
	session, exists := activeUploads[msg.RemotePath]
	if !exists {
		sendUploadError(conn, "Upload session not found")
		return
	}

	session.File.Close()

	stat, err := os.Stat(msg.RemotePath)
	if err != nil {
		sendUploadError(conn, fmt.Sprintf("Failed to verify uploaded file: %v", err))
		return
	}

	delete(activeUploads, msg.RemotePath)

	fmt.Printf("📤 Upload completed: %s (%d bytes)\n", msg.Filename, stat.Size())

	var output strings.Builder
	output.WriteString("Successfully uploaded file:\n")
	output.WriteString(fmt.Sprintf("\033[1;32m✅ %s\033[0m\n", msg.Filename))
	output.WriteString(fmt.Sprintf("📁 Remote path: \033[1;36m%s\033[0m\n", msg.RemotePath))
	output.WriteString(fmt.Sprintf("📏 Size: %d bytes\n", stat.Size()))

	response := UploadMessage{
		Type:       "upload_result",
		Command:    fmt.Sprintf("upload %s %s", msg.Filename, msg.RemotePath),
		Output:     output.String(),
		Filename:   msg.Filename,
		RemotePath: msg.RemotePath,
		FileSize:   stat.Size(),
		Uploaded:   true,
		IsComplete: true,
	}

	sendUploadResponse(conn, &response)
}

// sendUploadResponse sends a response message to the client
func sendUploadResponse(conn *websocket.Conn, response *UploadMessage) {
	msgBytes, err := json.Marshal(response)
	if err != nil {
		fmt.Printf("❌ Failed to marshal response: %v\n", err)
		return
	}

	if err := conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
		if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
			fmt.Printf("❌ WebSocket unexpected close during send: %v\n", err)
		} else {
			fmt.Printf("❌ Failed to send response: %v\n", err)
		}
	}
}

// sendUploadError sends an error message to the client
func sendUploadError(conn *websocket.Conn, errorMsg string) {
	response := UploadMessage{
		Type:  "error",
		Error: errorMsg,
	}

	msgBytes, err := json.Marshal(response)
	if err != nil {
		fmt.Printf("❌ Failed to marshal error response: %v\n", err)
		return
	}

	if err := conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
		if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
			fmt.Printf("❌ WebSocket unexpected close during error send: %v\n", err)
		} else {
			fmt.Printf("❌ Failed to send error response: %v\n", err)
		}
	}
}

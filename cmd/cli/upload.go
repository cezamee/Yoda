// Upload command implementation for the CLI client with chunked binary transfer
package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// UploadMessage structure for WebSocket communication (matches server)
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

// uploadCommand handles the upload command execution with chunked binary transfer
func uploadCommand(conn *websocket.Conn, localPath, remotePath string) {
	stat, err := os.Stat(localPath)
	if err != nil {
		fmt.Printf("❌ Error: cannot access '%s': %v\n", localPath, err)
		return
	}

	if stat.IsDir() {
		fmt.Printf("❌ Error: '%s' is a directory\n", localPath)
		return
	}

	file, err := os.Open(localPath)
	if err != nil {
		fmt.Printf("❌ Error: failed to open file '%s': %v\n", localPath, err)
		return
	}
	defer file.Close()

	const chunkSize = 80 * 1024
	totalChunks := int((stat.Size() + chunkSize - 1) / chunkSize)
	filename := filepath.Base(localPath)

	fmt.Printf("📤 Uploading '%s' (%d bytes) in %d chunks to '%s'...\n",
		filename, stat.Size(), totalChunks, remotePath)

	startMsg := UploadMessage{
		Type:        "upload_start",
		Filename:    filename,
		RemotePath:  remotePath,
		FileSize:    stat.Size(),
		TotalChunks: totalChunks,
		ChunkSize:   chunkSize,
	}

	if err := sendUploadMessage(conn, &startMsg); err != nil {
		fmt.Printf("❌ Failed to send start message: %v\n", err)
		return
	}

	for {
		conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		_, responseBytes, err := conn.ReadMessage()
		if err != nil {
			fmt.Printf("❌ Failed to read server response: %v\n", err)
			return
		}
		var response UploadMessage
		if err := json.Unmarshal(responseBytes, &response); err != nil {
			fmt.Printf("❌ Failed to parse server response: %v\n", err)
			return
		}
		if response.Type == "error" {
			fmt.Printf("❌ Server error: %s\n", response.Error)
			return
		}
		if response.Type == "upload_confirm" {
			fmt.Printf("⚠️  File '%s' already exists on the server. Overwrite? [y/N]: ", response.RemotePath)
			var answer string
			fmt.Scanln(&answer)
			answer = strings.ToLower(strings.TrimSpace(answer))
			if answer == "y" || answer == "yes" || answer == "o" || answer == "oui" {
				confirmMsg := UploadMessage{
					Type:       "upload_confirm_ok",
					RemotePath: response.RemotePath,
				}
				if err := sendUploadMessage(conn, &confirmMsg); err != nil {
					fmt.Printf("❌ Failed to send overwrite confirmation: %v\n", err)
					return
				}
				continue
			} else {
				cancelMsg := UploadMessage{
					Type:       "upload_confirm_cancel",
					RemotePath: response.RemotePath,
				}
				sendUploadMessage(conn, &cancelMsg)
				fmt.Printf("❌ Upload cancelled by user.\n")
				return
			}
		}
		if response.Type == "upload_ready" {
			break
		}
		fmt.Printf("❌ Unexpected response type: %s\n", response.Type)
		return
	}

	fmt.Printf("📤 Server ready, starting chunked transfer...\n")

	buffer := make([]byte, chunkSize)
	for chunkIndex := 0; chunkIndex < totalChunks; chunkIndex++ {
		bytesRead, err := file.Read(buffer)
		if err != nil && err != io.EOF {
			fmt.Printf("❌ Error reading chunk %d: %v\n", chunkIndex, err)
			return
		}

		chunkData := buffer[:bytesRead]

		if err := sendUploadChunk(conn, remotePath, filename, chunkIndex, totalChunks, chunkData); err != nil {
			fmt.Printf("❌ Failed to send chunk %d: %v\n", chunkIndex, err)
			return
		}

		if !waitForUploadAck(conn, "chunk_ack") {
			return
		}

		progress := float64(chunkIndex+1) / float64(totalChunks) * 100
		fmt.Printf("\r📤 Progress: %.1f%% (%d/%d chunks)", progress, chunkIndex+1, totalChunks)
	}
	fmt.Printf("\n")

	completeMsg := UploadMessage{
		Type:       "upload_complete",
		Filename:   filename,
		RemotePath: remotePath,
		FileSize:   stat.Size(),
		IsComplete: true,
	}

	if err := sendUploadMessage(conn, &completeMsg); err != nil {
		fmt.Printf("❌ Failed to send completion message: %v\n", err)
		return
	}

	waitForUploadResult(conn)
}

// sendUploadMessage sends a JSON message
func sendUploadMessage(conn *websocket.Conn, msg *UploadMessage) error {
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return conn.WriteMessage(websocket.TextMessage, msgBytes)
}

// sendUploadChunk sends a binary chunk with JSON header
func sendUploadChunk(conn *websocket.Conn, remotePath, filename string, chunkIndex, totalChunks int, chunkData []byte) error {
	conn.SetWriteDeadline(time.Now().Add(30 * time.Second))

	header := UploadMessage{
		Type:        "upload_chunk",
		Filename:    filename,
		RemotePath:  remotePath,
		ChunkIndex:  chunkIndex,
		TotalChunks: totalChunks,
		ChunkSize:   len(chunkData),
	}

	headerBytes, err := json.Marshal(header)
	if err != nil {
		return err
	}

	headerLen := len(headerBytes)
	message := make([]byte, 4+headerLen+len(chunkData))

	binary.LittleEndian.PutUint32(message[0:4], uint32(headerLen))

	copy(message[4:4+headerLen], headerBytes)

	copy(message[4+headerLen:], chunkData)

	return conn.WriteMessage(websocket.BinaryMessage, message)
}

// waitForUploadAck waits for acknowledgment
func waitForUploadAck(conn *websocket.Conn, expectedType string) bool {
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	_, responseBytes, err := conn.ReadMessage()
	if err != nil {
		fmt.Printf("❌ Failed to read acknowledgment: %v\n", err)
		return false
	}

	var response UploadMessage
	if err := json.Unmarshal(responseBytes, &response); err != nil {
		fmt.Printf("❌ Failed to parse acknowledgment: %v\n", err)
		return false
	}

	if response.Type == "error" {
		fmt.Printf("❌ Server error: %s\n", response.Error)
		return false
	}

	if response.Type != expectedType {
		fmt.Printf("❌ Unexpected response type: %s (expected %s)\n", response.Type, expectedType)
		return false
	}

	return true
}

// waitForUploadResult waits for the final upload result
func waitForUploadResult(conn *websocket.Conn) {
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	_, responseBytes, err := conn.ReadMessage()
	if err != nil {
		fmt.Printf("❌ Failed to read final result: %v\n", err)
		return
	}

	var response UploadMessage
	if err := json.Unmarshal(responseBytes, &response); err != nil {
		fmt.Printf("❌ Failed to parse final result: %v\n", err)
		return
	}

	switch response.Type {
	case "upload_result":
		fmt.Printf("🎉 Upload completed successfully!\n")
		fmt.Println("=" + strings.Repeat("=", 80))

		lines := strings.Split(response.Output, "\n")
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				fmt.Println(line)
			}
		}

		fmt.Println("=" + strings.Repeat("=", 80))

		if response.Uploaded {
			fmt.Printf("✅ File uploaded successfully!\n")
		}
	case "error":
		fmt.Printf("❌ Upload failed: %s\n", response.Error)
	default:
		fmt.Printf("❌ Unknown response type: %s\n", response.Type)
	}

	conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
}

// Upload command implementation for the CLI client using HTTP PUT
package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/cezamee/Yoda/cmd/cli/net"
)

func UploadCommand(args []string) {
	// Handle Ctrl+C interruption with context
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Parse arguments
	if len(args) < 2 {
		fmt.Println("❌ Error: missing arguments")
		return
	}
	localPath := args[0]
	remotePath := args[1]

	// Check local file
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

	filename := filepath.Base(localPath)
	query := fmt.Sprintf("/upload?path=%s", remotePath)
	fmt.Printf("📤 Uploading '%s' (%d bytes) to '%s'...\n", filename, stat.Size(), remotePath)
	startTime := time.Now()

	pr, pipeWriter := io.Pipe()
	done := make(chan error, 1)

	// Setup progressWriter for upload
	var total int64 = 0
	var size int64 = stat.Size()
	showProgress := size > 0
	lastPrint := time.Now()
	pw := &net.ProgressWriter{
		Out:          pipeWriter,
		Total:        &total,
		Size:         size,
		StartTime:    startTime,
		LastPrint:    &lastPrint,
		ShowProgress: showProgress,
	}

	// Use io.TeeReader to track progress while uploading
	go func() {
		buf := make([]byte, 1024*1024) // 1MB buffer
		reader := io.TeeReader(file, pw)
		errCh := make(chan error, 1)
		go func() {
			_, err := io.CopyBuffer(pipeWriter, reader, buf)
			errCh <- err
		}()
		select {
		case <-ctx.Done():
			pipeWriter.Close()
			done <- ctx.Err()
		case err := <-errCh:
			pipeWriter.Close()
			done <- err
		}
	}()

	// Send file to server
	resp, err := net.CreateSecureHTTPClient("PUT", query, pr)
	if err != nil {
		fmt.Printf("❌ Upload failed: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		pipeWriter.Close()
		fmt.Printf("\n❌ Upload failed: file already exists on server (%s)\n", remotePath)
		return
	}

	// Wait for upload to finish or cancellation
	select {
	case <-ctx.Done():
		pipeWriter.Close()
		fmt.Println("\n❌ Upload cancelled (Ctrl+C), local file kept.")
		return
	case err := <-done:
		if err != nil && err != io.EOF {
			fmt.Printf("❌ Error during upload: %v\n", err)
			return
		}
	}

	// Final progress display
	if showProgress {
		percent := float64(total) / float64(size)
		elapsed := time.Since(startTime).Seconds()
		speed := float64(total) / (1024 * 1024) / elapsed
		fmt.Printf("\r%.0f%% - %.2f MB/s", percent*100, speed)
	}
	fmt.Println()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("❌ Server error: %s\n%s\n", resp.Status, string(body))
		return
	}

	// Print upload summary
	elapsed := time.Since(startTime).Seconds()
	speed := float64(stat.Size()) / 1024.0 / 1024.0 / elapsed
	fmt.Printf("✅ Upload completed: %d bytes in %.2f seconds (%.2f MB/s)\n", stat.Size(), elapsed, speed)
}

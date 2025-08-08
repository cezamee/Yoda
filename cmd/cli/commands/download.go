// File download client: handles file download from server via HTTP GET

package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/cezamee/Yoda/cmd/cli/net"
)

func DownloadCommand(args []string) {
	// Parse arguments
	remotePath := args[0]
	localPath := args[1]

	// Handle Ctrl+C interruption with context
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Check if local file exists
	if _, err := os.Stat(localPath); err == nil {
		fmt.Printf("⚠️ Local file '%s' already exists. Overwrite? (y/N): ", localPath)
		var response string
		fmt.Scanln(&response)
		if response != "y" && response != "Y" && response != "yes" {
			fmt.Println("❌ Download cancelled")
			return
		}
	}

	// Request file from server
	query := fmt.Sprintf("/download?path=%s", remotePath)
	resp, err := net.CreateSecureHTTPClient("GET", query, nil)
	if err != nil {
		fmt.Printf("❌ Download failed: %v\n", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fmt.Printf("❌ Download failed: server returned status %d\n", resp.StatusCode)
		return
	}

	// Create local file
	out, err := os.Create(localPath)
	if err != nil {
		fmt.Printf("❌ Cannot create local file: %v\n", err)
		return
	}
	defer func() {
		out.Close()
	}()

	// Print file size info
	var size int64 = resp.ContentLength
	if size > 0 {
		fmt.Printf("Downloading %.2f MB (%d bytes)\n", float64(size)/(1024*1024), size)
	} else {
		fmt.Println("Downloading (unknown size)...")
	}

	// Setup progress and buffer
	buf := make([]byte, 1024*1024)
	var total int64 = 0
	showProgress := size > 0
	startTime := time.Now()
	lastPrint := time.Now()
	pw := &net.ProgressWriter{
		Out:          out,
		Total:        &total,
		Size:         size,
		StartTime:    startTime,
		LastPrint:    &lastPrint,
		ShowProgress: showProgress,
	}
	reader := io.TeeReader(resp.Body, pw)

	// Download loop with context cancellation
	done := make(chan error, 1)
	go func() {
		_, err := io.CopyBuffer(io.Discard, reader, buf)
		done <- err
	}()
	select {
	case <-ctx.Done():
		resp.Body.Close()
		out.Close()
		os.Remove(localPath)
		fmt.Println("\n❌ Download cancelled (Ctrl+C), file deleted.")
		return
	case err := <-done:
		if err != nil && err != io.EOF {
			fmt.Printf("❌ Error reading file: %v\n", err)
			return
		}
		if showProgress {
			percent := float64(total) / float64(size)
			elapsed := time.Since(startTime).Seconds()
			speed := float64(total) / (1024 * 1024) / elapsed
			fmt.Printf("\r%.0f%% - %.2f MB/s", percent*100, speed)
		}
		fmt.Printf("\n✅ Downloaded to %s\n", localPath)
	}
}

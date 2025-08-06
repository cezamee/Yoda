// Upload command implementation for the CLI client using HTTP PUT
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"
)

func uploadCommand(args []string) {
	interrupted := false
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	cancelChan := make(chan struct{})

	go func() {
		<-sigChan
		interrupted = true
		close(cancelChan)
	}()

	if len(args) < 2 {
		fmt.Println("❌ Error: missing arguments")
		return
	}

	localPath := args[0]
	remotePath := args[1]

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

	pr, pw := io.Pipe()
	done := make(chan error, 1)

	go func() {
		buf := make([]byte, 256*1024)
		var total int64 = 0
		var size int64 = stat.Size()
		showProgress := size > 0

		for {
			select {
			case <-cancelChan:
				pw.Close()
				done <- fmt.Errorf("upload cancelled by user (Ctrl+C)")
				return
			default:
				n, err := file.Read(buf)
				if n > 0 {
					written, werr := pw.Write(buf[:n])
					if werr != nil {
						done <- werr
						return
					}
					total += int64(written)
					if showProgress {
						percent := float64(total) / float64(size)
						elapsed := time.Since(startTime).Seconds()
						if elapsed <= 0 {
							elapsed = 1e-3
						}
						speed := float64(total) / (1024 * 1024) / elapsed
						fmt.Printf("\r%.0f%% - %.2f MB/s", percent*100, speed)
					}
				}
				if err == io.EOF {
					pw.Close()
					done <- nil
					return
				}
				if err != nil {
					done <- err
					return
				}
			}
		}
	}()

	resp, err := createSecureHTTPClient("PUT", query, pr)
	if err != nil {
		fmt.Printf("❌ Upload failed: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		pw.Close()
		fmt.Printf("\n❌ Upload failed: file already exists on server (%s)\n", remotePath)
		return
	}

	if err := <-done; err != nil {
		if interrupted {
			fmt.Printf("\n❌ Upload cancelled (Ctrl+C), local file kept.\n")
		} else {
			fmt.Printf("❌ Error during upload: %v\n", err)
		}
		return
	}

	fmt.Printf("\n")
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("❌ Server error: %s\n%s\n", resp.Status, string(body))
		return
	}

	elapsed := time.Since(startTime).Seconds()
	speed := float64(stat.Size()) / 1024.0 / 1024.0 / elapsed
	fmt.Printf("✅ Upload completed: %d bytes in %.2f seconds (%.2f MB/s)\n", stat.Size(), elapsed, speed)
}

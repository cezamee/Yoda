// File download client: handles file download from server via HTTP GET

package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"
)

func downloadCommand(args []string) {
	remotePath := args[0]
	localPath := args[1]

	interrupted := false
	sigChan := make(chan os.Signal, 1)
	doneChan := make(chan struct{})
	signal.Notify(sigChan, os.Interrupt)

	go func() {
		<-sigChan
		interrupted = true
		close(doneChan)
	}()

	if _, err := os.Stat(localPath); err == nil {
		fmt.Printf("⚠️ Local file '%s' already exists. Overwrite? (y/N): ", localPath)
		var response string
		fmt.Scanln(&response)
		if response != "y" && response != "Y" && response != "yes" {
			fmt.Printf("❌ Download cancelled\n")
			return
		}
	}

	query := fmt.Sprintf("/download?path=%s", remotePath)
	resp, err := createSecureHTTPClient("GET", query, nil)
	if err != nil {
		fmt.Printf("❌ Download failed: %v\n", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fmt.Printf("❌ Download failed: server returned status %d\n", resp.StatusCode)
		return
	}
	out, err := os.Create(localPath)
	if err != nil {
		fmt.Printf("❌ Cannot create local file: %v\n", err)
		return
	}
	defer func() {
		out.Close()
		if interrupted {
			os.Remove(localPath)
			fmt.Printf("\n❌ Téléchargement annulé (Ctrl+C), fichier supprimé.\n")
		}
	}()

	if resp.ContentLength <= 0 {
		fmt.Println("Downloading (unknown size)...")
	}

	buf := make([]byte, 256*1024)
	var total int64 = 0
	var size int64 = resp.ContentLength
	showProgress := size > 0

	if showProgress {
		fmt.Printf("Downloading %.2f MB\n", float64(size)/(1024*1024))
	}

	startTime := time.Now()
	completed := false

	for {
		select {
		case <-doneChan:
			return
		default:
			n, err := resp.Body.Read(buf)

			if n > 0 {
				written, werr := out.Write(buf[:n])
				if werr != nil {
					fmt.Printf("❌ Error writing file: %v\n", werr)
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
				completed = true
				break
			}

			if err != nil {
				fmt.Printf("❌ Error reading file: %v\n", err)
				return
			}
		}

		if completed {
			break
		}
	}

	if !interrupted {
		fmt.Printf("\n")
		fmt.Printf("✅ Downloaded to %s\n", localPath)
	}
}

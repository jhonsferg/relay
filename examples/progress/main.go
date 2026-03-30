// Package main demonstrates relay's upload and download progress callbacks.
// Progress reporting is useful for large file transfers where you want to
// display a progress bar, update a UI, log throughput, or enforce transfer
// deadlines based on bytes moved rather than elapsed time.
package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	relay "github.com/jhonsferg/relay"
)

// progressBar renders a simple ASCII progress bar to stdout.
func progressBar(label string, transferred, total int64) {
	if total <= 0 {
		fmt.Printf("\r%s: %d bytes transferred (total unknown)", label, transferred)
		return
	}
	pct := float64(transferred) / float64(total) * 100
	filled := int(pct / 5) // 20 chars = 100%
	bar := strings.Repeat("█", filled) + strings.Repeat("░", 20-filled)
	fmt.Printf("\r%s: [%s] %.0f%% (%d/%d bytes)", label, bar, pct, transferred, total)
	if transferred >= total {
		fmt.Println()
	}
}

func main() {
	// -------------------------------------------------------------------------
	// 1. Download progress.
	//
	// The server streams a 500 KB payload in small chunks.
	// WithDownloadProgress is called after each chunk arrives with the
	// cumulative bytes received and the Content-Length total.
	// -------------------------------------------------------------------------
	const downloadSize = 500 * 1024 // 500 KB

	downloadSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", downloadSize))
		chunk := make([]byte, 8*1024) // 8 KB chunks
		remaining := downloadSize
		for remaining > 0 {
			n := remaining
			if n > len(chunk) {
				n = len(chunk)
			}
			w.Write(chunk[:n]) //nolint:errcheck
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			remaining -= n
			time.Sleep(2 * time.Millisecond) // simulate network pacing
		}
	}))
	defer downloadSrv.Close()

	fmt.Println("=== Download progress ===")
	client := relay.New()

	start := time.Now()
	resp, err := client.Execute(
		client.Get(downloadSrv.URL).
			WithDownloadProgress(func(transferred, total int64) {
				progressBar("download", transferred, total)
			}),
	)
	if err != nil {
		log.Fatalf("download failed: %v", err)
	}
	elapsed := time.Since(start)
	fmt.Printf("  downloaded %d bytes in %s\n\n", len(resp.Body()), elapsed.Round(time.Millisecond))

	// -------------------------------------------------------------------------
	// 2. Upload progress.
	//
	// WithUploadProgress is called during request body transmission.
	// Here we upload 200 KB of synthetic data.
	// -------------------------------------------------------------------------
	const uploadSize = 200 * 1024 // 200 KB

	uploadSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := io.Copy(io.Discard, r.Body)
		fmt.Fprintf(w, `{"received":%d}`, n)
	}))
	defer uploadSrv.Close()

	fmt.Println("=== Upload progress ===")
	payload := make([]byte, uploadSize)
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	start = time.Now()
	resp, err = client.Execute(
		client.Post(uploadSrv.URL).
			WithBody(payload).
			WithContentType("application/octet-stream").
			WithUploadProgress(func(transferred, total int64) {
				progressBar("upload  ", transferred, total)
			}),
	)
	if err != nil {
		log.Fatalf("upload failed: %v", err)
	}
	elapsed = time.Since(start)
	fmt.Printf("  server received: %s  (took %s)\n\n", resp.String(), elapsed.Round(time.Millisecond))

	// -------------------------------------------------------------------------
	// 3. Multipart file upload with progress.
	//
	// WithMultipart builds the multipart/form-data body; WithUploadProgress
	// tracks the total bytes sent across all parts.
	// -------------------------------------------------------------------------
	multiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseMultipartForm(10 << 20) //nolint:errcheck
		var parts []string
		for name := range r.MultipartForm.File {
			fh := r.MultipartForm.File[name][0]
			parts = append(parts, fmt.Sprintf("%s(%d bytes)", fh.Filename, fh.Size))
		}
		fmt.Fprintf(w, `{"parts":%q}`, parts)
	}))
	defer multiSrv.Close()

	fmt.Println("=== Multipart upload progress ===")
	file1 := make([]byte, 80*1024)  // 80 KB "file"
	file2 := make([]byte, 120*1024) // 120 KB "file"

	resp, err = client.Execute(
		client.Post(multiSrv.URL).
			WithMultipart([]relay.MultipartField{
				{
					FieldName:   "report",
					FileName:    "report.csv",
					ContentType: "text/csv",
					Content:     file1,
				},
				{
					FieldName:   "attachment",
					FileName:    "image.png",
					ContentType: "image/png",
					Content:     file2,
				},
				{
					FieldName: "description",
					Content:   []byte("monthly sales report"),
				},
			}).
			WithUploadProgress(func(transferred, total int64) {
				progressBar("multipart", transferred, total)
			}),
	)
	if err != nil {
		log.Fatalf("multipart upload failed: %v", err)
	}
	fmt.Printf("  server response: %s\n\n", resp.String())

	// -------------------------------------------------------------------------
	// 4. Combined upload + download progress on the same request.
	//
	// Both callbacks can be set simultaneously; they fire independently.
	// -------------------------------------------------------------------------
	echoSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		w.Write(body) //nolint:errcheck
	}))
	defer echoSrv.Close()

	fmt.Println("=== Combined upload + download progress ===")
	echoPayload := make([]byte, 50*1024) // 50 KB echo

	resp, err = client.Execute(
		client.Post(echoSrv.URL).
			WithBody(echoPayload).
			WithUploadProgress(func(t, total int64) {
				progressBar("↑ upload  ", t, total)
			}).
			WithDownloadProgress(func(t, total int64) {
				progressBar("↓ download", t, total)
			}),
	)
	if err != nil {
		log.Fatalf("echo request failed: %v", err)
	}
	fmt.Printf("  echo round-trip: %d bytes sent and received\n", len(resp.Body()))
}

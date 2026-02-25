package downloader

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const benchmarkLargePayloadSize int64 = 64 * 1024 * 1024

func BenchmarkProfileDownloaderSingleLargeFile(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", benchmarkLargePayloadSize))
		_, _ = io.CopyN(w, rand.New(rand.NewSource(42)), benchmarkLargePayloadSize)
	}))
	defer server.Close()

	d := New(server.Client())
	workDir := b.TempDir()

	b.ReportAllocs()
	b.SetBytes(benchmarkLargePayloadSize)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		target := filepath.Join(workDir, fmt.Sprintf("single-large-%d.bin", i))
		_, err := d.Download(context.Background(), Request{
			URL:         server.URL + "/large.bin",
			OutputPath:  target,
			Timeout:     60 * time.Second,
			MaxRetries:  0,
			BackoffBase: time.Millisecond,
			BackoffMax:  time.Millisecond,
		})
		if err != nil {
			b.Fatalf("Download returned error: %v", err)
		}
		_ = os.Remove(target)
		_ = os.Remove(target + ".part")
	}
}

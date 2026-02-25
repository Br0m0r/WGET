package mirror

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	benchmarkMirrorPages         = 80
	benchmarkMirrorAssetsPerPage = 4
	benchmarkMirrorAssetBytes    = 4096
)

func BenchmarkProfileMirrorSyntheticSite(b *testing.B) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/robots.txt":
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "User-agent: *\nDisallow:\n")
			return
		case r.URL.Path == "/":
			serveBenchmarkPage(w, 0, server.URL)
			return
		case strings.HasPrefix(r.URL.Path, "/page/") && strings.HasSuffix(r.URL.Path, ".html"):
			idStr := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/page/"), ".html")
			id, err := strconv.Atoi(idStr)
			if err != nil || id < 0 || id >= benchmarkMirrorPages {
				http.NotFound(w, r)
				return
			}
			serveBenchmarkPage(w, id, server.URL)
			return
		case strings.HasPrefix(r.URL.Path, "/assets/"):
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(strings.Repeat("x", benchmarkMirrorAssetBytes)))
			return
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	engine := NewEngine(server.Client())
	totalBytesEstimate := int64(benchmarkMirrorPages * benchmarkMirrorAssetsPerPage * benchmarkMirrorAssetBytes)
	baseOut := b.TempDir()

	b.ReportAllocs()
	b.SetBytes(totalBytesEstimate)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		outDir := filepath.Join(baseOut, fmt.Sprintf("run-%d", i))
		summary, err := engine.Run(context.Background(), []string{server.URL + "/"}, Config{
			OutputDir:         outDir,
			MaxDepth:          benchmarkMirrorPages + 5,
			MaxPages:          benchmarkMirrorPages + 10,
			MaxTotalBytes:     1 << 30,
			AllowSchemeChange: true,
			RespectRobots:     true,
		})
		if err != nil {
			b.Fatalf("Run returned error: %v", err)
		}
		if summary.PagesVisited < benchmarkMirrorPages {
			b.Fatalf("unexpected page count: got %d want >= %d", summary.PagesVisited, benchmarkMirrorPages)
		}
		_ = os.RemoveAll(outDir)
	}
}

func serveBenchmarkPage(w http.ResponseWriter, id int, base string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	next := id + 1
	prev := id - 1
	var b strings.Builder
	b.WriteString("<html><body>\n")
	if next < benchmarkMirrorPages {
		fmt.Fprintf(&b, `<a href="%s/page/%d.html">next</a>`+"\n", base, next)
	}
	if prev >= 0 {
		fmt.Fprintf(&b, `<a href="%s/page/%d.html">prev</a>`+"\n", base, prev)
	}
	for i := 0; i < benchmarkMirrorAssetsPerPage; i++ {
		fmt.Fprintf(&b, `<img src="%s/assets/%d_%d.bin">`+"\n", base, id, i)
	}
	b.WriteString("</body></html>")
	_, _ = w.Write([]byte(b.String()))
}

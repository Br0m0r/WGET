package mirror

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"wget/internal/errcode"
)

func TestEngine_RecursionCorrectness(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<html><body>
<a href="/a/">A</a>
<a href="%s/b.html">B</a>
<link rel="stylesheet" href="/assets/site.css">
<a href="https://outside.example.org/ignore">external</a>
</body></html>`, srv.URL)
		case "/a/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><body><a href="/">home</a></body></html>`)
		case "/b.html":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><body><a href="/a/">A</a><img src="/assets/logo.png"></body></html>`)
		case "/assets/site.css":
			w.Header().Set("Content-Type", "text/css")
			fmt.Fprint(w, "body{color:#111}")
		case "/assets/logo.png":
			w.Header().Set("Content-Type", "image/png")
			fmt.Fprint(w, "PNGDATA")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	outDir := t.TempDir()
	engine := NewEngine(srv.Client())
	summary, err := engine.Run(context.Background(), []string{srv.URL + "/"}, Config{
		OutputDir:         outDir,
		MaxDepth:          5,
		MaxPages:          10,
		MaxTotalBytes:     1 << 20,
		AllowSchemeChange: true,
		RespectRobots:     true,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if summary.PagesVisited != 3 {
		t.Fatalf("expected 3 pages visited, got %d", summary.PagesVisited)
	}
	if summary.ResourcesDownloaded < 5 {
		t.Fatalf("expected at least 5 resources downloaded, got %d", summary.ResourcesDownloaded)
	}

	host := strings.Split(strings.TrimPrefix(srv.URL, "http://"), ":")[0]
	paths := []string{
		filepath.Join(outDir, host, "index.html"),
		filepath.Join(outDir, host, "a", "index.html"),
		filepath.Join(outDir, host, "b.html"),
		filepath.Join(outDir, host, "assets", "site.css"),
		filepath.Join(outDir, host, "assets", "logo.png"),
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected mirrored file missing: %s err=%v", p, err)
		}
	}

	outsideDir := filepath.Join(outDir, "outside.example.org")
	if _, err := os.Stat(outsideDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("external domain should not be mirrored, stat err=%v", err)
	}
}

func TestEngine_MaxPagesCap(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<a href="/a">a</a>`)
		case "/a":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<a href="/b">b</a>`)
		case "/b":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<a href="/c">c</a>`)
		default:
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html></html>`)
		}
	}))
	defer server.Close()

	engine := NewEngine(server.Client())
	_, err := engine.Run(context.Background(), []string{server.URL + "/"}, Config{
		OutputDir:         t.TempDir(),
		MaxDepth:          5,
		MaxPages:          2,
		MaxTotalBytes:     1 << 20,
		AllowSchemeChange: true,
		RespectRobots:     true,
	})
	if !errors.Is(err, ErrMaxPagesExceeded) {
		t.Fatalf("expected ErrMaxPagesExceeded, got %v", err)
	}
}

func TestEngine_MaxTotalBytesCap(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, strings.Repeat("x", 2048))
	}))
	defer server.Close()

	engine := NewEngine(server.Client())
	_, err := engine.Run(context.Background(), []string{server.URL + "/"}, Config{
		OutputDir:         t.TempDir(),
		MaxDepth:          1,
		MaxPages:          5,
		MaxTotalBytes:     512,
		AllowSchemeChange: true,
		RespectRobots:     true,
	})
	if !errors.Is(err, ErrMaxTotalBytesExceeded) {
		t.Fatalf("expected ErrMaxTotalBytesExceeded, got %v", err)
	}
}

func TestEngine_RespectsRobotsSimpleDisallow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "User-agent: *\nDisallow: /private\n")
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<a href="/private/page.html">private</a><a href="/public/page.html">public</a>`)
		case "/public/page.html":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html>ok</html>`)
		case "/private/page.html":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html>private</html>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	outDir := t.TempDir()
	engine := NewEngine(server.Client())
	_, err := engine.Run(context.Background(), []string{server.URL + "/"}, Config{
		OutputDir:         outDir,
		MaxDepth:          5,
		MaxPages:          10,
		MaxTotalBytes:     1 << 20,
		AllowSchemeChange: true,
		RespectRobots:     true,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	host := strings.Split(strings.TrimPrefix(server.URL, "http://"), ":")[0]
	publicPath := filepath.Join(outDir, host, "public", "page.html")
	if _, err := os.Stat(publicPath); err != nil {
		t.Fatalf("expected public page mirrored, err=%v", err)
	}

	privatePath := filepath.Join(outDir, host, "private", "page.html")
	// Give filesystem a moment only in case of parallel writes; mirror loop is sync but keep deterministic.
	time.Sleep(10 * time.Millisecond)
	if _, err := os.Stat(privatePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected private path to be skipped due to robots, stat err=%v", err)
	}
}

func TestEngine_RobotsFailureNonFatalByDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<a href="/ok.html">ok</a>`)
		case "/ok.html":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html>ok</html>`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	baseClient := server.Client()
	baseRT := baseClient.Transport
	if baseRT == nil {
		baseRT = http.DefaultTransport
	}
	client := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/robots.txt" {
				return nil, errors.New("simulated robots fetch failure")
			}
			return baseRT.RoundTrip(req)
		}),
		Timeout: baseClient.Timeout,
	}

	engine := NewEngine(client)
	_, err := engine.Run(context.Background(), []string{server.URL + "/"}, Config{
		OutputDir:         t.TempDir(),
		MaxDepth:          3,
		MaxPages:          10,
		MaxTotalBytes:     1 << 20,
		AllowSchemeChange: true,
		RespectRobots:     true,
		StrictRobots:      false,
	})
	if err != nil {
		t.Fatalf("expected non-fatal robots behavior, got %v", err)
	}
}

func TestEngine_RobotsFailureStrictMode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html>ok</html>`)
	}))
	defer server.Close()

	baseClient := server.Client()
	baseRT := baseClient.Transport
	if baseRT == nil {
		baseRT = http.DefaultTransport
	}
	client := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/robots.txt" {
				return nil, errors.New("simulated robots fetch failure")
			}
			return baseRT.RoundTrip(req)
		}),
		Timeout: baseClient.Timeout,
	}

	engine := NewEngine(client)
	_, err := engine.Run(context.Background(), []string{server.URL + "/"}, Config{
		OutputDir:         t.TempDir(),
		MaxDepth:          3,
		MaxPages:          10,
		MaxTotalBytes:     1 << 20,
		AllowSchemeChange: true,
		RespectRobots:     true,
		StrictRobots:      true,
	})
	if err == nil {
		t.Fatal("expected strict robots failure")
	}
	var robotsErr *RobotsError
	if !errors.As(err, &robotsErr) {
		t.Fatalf("expected RobotsError, got %T", err)
	}
	if robotsErr.ErrorCode() != errcode.CodeRobotsFetch {
		t.Fatalf("unexpected robots error code: got %s want %s", robotsErr.ErrorCode(), errcode.CodeRobotsFetch)
	}
}

func TestEngine_AggregateErrorIncludesCodes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<a href="/ok.html">ok</a><a href="/bad.html">bad</a>`)
		case "/ok.html":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html>ok</html>`)
		case "/bad.html":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	engine := NewEngine(server.Client())
	_, err := engine.Run(context.Background(), []string{server.URL + "/"}, Config{
		OutputDir:         t.TempDir(),
		MaxDepth:          3,
		MaxPages:          10,
		MaxTotalBytes:     1 << 20,
		AllowSchemeChange: true,
		RespectRobots:     true,
	})
	if err == nil {
		t.Fatal("expected aggregate error")
	}
	var aggErr *AggregateError
	if !errors.As(err, &aggErr) {
		t.Fatalf("expected AggregateError, got %T", err)
	}
	if len(aggErr.Errors) == 0 {
		t.Fatal("expected at least one crawl error")
	}
	found5xx := false
	for _, cErr := range aggErr.Errors {
		if cErr.Code == errcode.CodeHTTP5XX {
			found5xx = true
			break
		}
	}
	if !found5xx {
		t.Fatalf("expected at least one %s failure code, got %#v", errcode.CodeHTTP5XX, aggErr.Errors)
	}
}

func TestEngine_FilterCorrectness(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><body>
<a href="/docs/index.html">docs</a>
<a href="/admin/secret.html">admin</a>
<img src="/img/logo.png">
<img src="/img/photo.jpg">
<img src="/img/anim.gif">
</body></html>`)
		case "/docs/index.html":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html>docs</html>`)
		case "/admin/secret.html":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html>secret</html>`)
		case "/img/logo.png":
			w.Header().Set("Content-Type", "image/png")
			fmt.Fprint(w, "PNG")
		case "/img/photo.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			fmt.Fprint(w, "JPG")
		case "/img/anim.gif":
			w.Header().Set("Content-Type", "image/gif")
			fmt.Fprint(w, "GIF")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	outDir := t.TempDir()
	engine := NewEngine(server.Client())
	_, err := engine.Run(context.Background(), []string{server.URL + "/"}, Config{
		OutputDir:         outDir,
		MaxDepth:          5,
		MaxPages:          10,
		MaxTotalBytes:     1 << 20,
		AllowSchemeChange: true,
		RespectRobots:     true,
		RejectPatterns:    []string{".jpg", "*.gif"},
		ExcludeDirs:       []string{"/admin"},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	host := strings.Split(strings.TrimPrefix(server.URL, "http://"), ":")[0]

	mustExist := []string{
		filepath.Join(outDir, host, "index.html"),
		filepath.Join(outDir, host, "docs", "index.html"),
		filepath.Join(outDir, host, "img", "logo.png"),
	}
	for _, p := range mustExist {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected file to exist: %s err=%v", p, err)
		}
	}

	mustNotExist := []string{
		filepath.Join(outDir, host, "admin", "secret.html"),
		filepath.Join(outDir, host, "img", "photo.jpg"),
		filepath.Join(outDir, host, "img", "anim.gif"),
	}
	for _, p := range mustNotExist {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected file to be filtered out: %s err=%v", p, err)
		}
	}
}

func TestEngine_DownloadsInlineCSSURLAssets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><head><style>body{background:url('/assets/bg.png')}</style></head><body>ok</body></html>`)
		case "/assets/bg.png":
			w.Header().Set("Content-Type", "image/png")
			fmt.Fprint(w, "PNGDATA")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	outDir := t.TempDir()
	engine := NewEngine(server.Client())
	_, err := engine.Run(context.Background(), []string{server.URL + "/"}, Config{
		OutputDir:         outDir,
		MaxDepth:          3,
		MaxPages:          10,
		MaxTotalBytes:     1 << 20,
		AllowSchemeChange: true,
		RespectRobots:     true,
		ConvertLinks:      true,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	host := strings.Split(strings.TrimPrefix(server.URL, "http://"), ":")[0]
	bgPath := filepath.Join(outDir, host, "assets", "bg.png")
	if _, err := os.Stat(bgPath); err != nil {
		t.Fatalf("expected css-referenced asset to be mirrored, err=%v", err)
	}

	rootPath := filepath.Join(outDir, host, "index.html")
	rootContent, err := os.ReadFile(rootPath)
	if err != nil {
		t.Fatalf("read mirrored root: %v", err)
	}
	rootText := string(rootContent)
	if !strings.Contains(rootText, `url("assets/bg.png")`) && !strings.Contains(rootText, `url('assets/bg.png')`) && !strings.Contains(rootText, `url(assets/bg.png)`) {
		t.Fatalf("expected converted css url in mirrored html, got: %s", rootText)
	}
}

func TestEngine_ConvertLinksCorrectness(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><body>
<style>body{background-image:url('/assets/logo.png')}</style>
<a href="/docs/page.html">Doc</a>
<img src="/assets/logo.png">
<a href="https://outside.example.org/ext.html">Ext</a>
</body></html>`)
		case "/docs/page.html":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><body>
<a href="/">Home</a>
<a href="/assets/logo.png#v">Logo</a>
</body></html>`)
		case "/assets/logo.png":
			w.Header().Set("Content-Type", "image/png")
			fmt.Fprint(w, "PNGDATA")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	outDir := t.TempDir()
	engine := NewEngine(server.Client())
	_, err := engine.Run(context.Background(), []string{server.URL + "/"}, Config{
		OutputDir:         outDir,
		MaxDepth:          5,
		MaxPages:          10,
		MaxTotalBytes:     1 << 20,
		AllowSchemeChange: true,
		RespectRobots:     true,
		ConvertLinks:      true,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	host := strings.Split(strings.TrimPrefix(server.URL, "http://"), ":")[0]
	rootFile := filepath.Join(outDir, host, "index.html")
	docFile := filepath.Join(outDir, host, "docs", "page.html")

	rootContent, err := os.ReadFile(rootFile)
	if err != nil {
		t.Fatalf("read root html: %v", err)
	}
	docContent, err := os.ReadFile(docFile)
	if err != nil {
		t.Fatalf("read doc html: %v", err)
	}

	rootText := string(rootContent)
	docText := string(docContent)

	if !strings.Contains(rootText, `href="docs/page.html"`) {
		t.Fatalf("expected converted doc link in root html, got: %s", rootText)
	}
	if !strings.Contains(rootText, `src="assets/logo.png"`) {
		t.Fatalf("expected converted asset src in root html, got: %s", rootText)
	}
	if !strings.Contains(rootText, `url("assets/logo.png")`) && !strings.Contains(rootText, `url('assets/logo.png')`) && !strings.Contains(rootText, `url(assets/logo.png)`) {
		t.Fatalf("expected converted css url in root html, got: %s", rootText)
	}
	if !strings.Contains(rootText, `href="https://outside.example.org/ext.html"`) {
		t.Fatalf("expected external link unchanged, got: %s", rootText)
	}

	if !strings.Contains(docText, `href="../index.html"`) {
		t.Fatalf("expected converted back-link in doc html, got: %s", docText)
	}
	if !strings.Contains(docText, `href="../assets/logo.png#v"`) {
		t.Fatalf("expected converted anchor asset link in doc html, got: %s", docText)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

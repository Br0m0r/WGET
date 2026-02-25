package mirror

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/net/publicsuffix"

	"wget/internal/errcode"
	"wget/internal/parser"
)

const (
	defaultMaxDepth = 5
	defaultMaxPages = 10_000
)

var (
	// ErrMaxPagesExceeded is returned when configured page cap is hit.
	ErrMaxPagesExceeded = errors.New("mirror max pages exceeded")
	// ErrMaxTotalBytesExceeded is returned when configured byte cap is hit.
	ErrMaxTotalBytesExceeded = errors.New("mirror max total bytes exceeded")
)

var mirrorCopyBufferPool = sync.Pool{
	New: func() any {
		return make([]byte, 128*1024)
	},
}

var mirrorPathSegmentReplacer = strings.NewReplacer(
	"/", "_",
	"\\", "_",
	":", "_",
	"*", "_",
	"?", "_",
	"\"", "_",
	"<", "_",
	">", "_",
	"|", "_",
)

func init() {
	mirrorCopyBufferPool.Put(make([]byte, 128*1024))
}

// Config controls crawler behavior.
type Config struct {
	OutputDir         string
	MaxDepth          int
	MaxPages          int
	MaxTotalBytes     int64
	RejectPatterns    []string
	ExcludeDirs       []string
	ConvertLinks      bool
	AllowSchemeChange bool
	RespectRobots     bool
	StrictRobots      bool
	UserAgent         string
}

// Summary captures crawl results.
type Summary struct {
	PagesVisited        int
	ResourcesDownloaded int
	TotalBytes          int64
	VisitedURLs         int
}

// CrawlError is one failed URL fetch.
type CrawlError struct {
	URL  string
	Code string
	Err  error
}

// AggregateError groups non-fatal crawl failures.
type AggregateError struct {
	Errors []CrawlError
}

func (e *AggregateError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d mirror download(s) failed", len(e.Errors))
	for _, item := range e.Errors {
		fmt.Fprintf(&b, " [url=%s code=%s err=%v]", item.URL, item.Code, item.Err)
	}
	return b.String()
}

// RobotsError is returned when strict robots mode is enabled and robots rules cannot be fetched/parsed.
type RobotsError struct {
	URL   string
	Cause error
}

func (e *RobotsError) Error() string {
	return fmt.Sprintf("robots.txt check failed for %s: %v", e.URL, e.Cause)
}

func (e *RobotsError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *RobotsError) ErrorCode() string {
	return errcode.CodeRobotsFetch
}

type crawlItem struct {
	url   string
	depth int
}

type robotsRules struct {
	disallow []string
}

// HTTPStatusError reports non-success HTTP responses during mirror fetch.
type HTTPStatusError struct {
	Code int
	URL  string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("unexpected HTTP status %d", e.Code)
}

func (e *HTTPStatusError) ErrorCode() string {
	return errcode.HTTPStatus(e.Code)
}

// Engine mirrors websites recursively.
type Engine struct {
	client *http.Client

	robotsMu sync.Mutex
	robots   map[string]robotsRules
}

// NewEngine constructs a mirror engine.
func NewEngine(client *http.Client) *Engine {
	if client == nil {
		client = &http.Client{}
	}
	return &Engine{
		client: client,
		robots: make(map[string]robotsRules),
	}
}

// Run crawls seeds recursively and saves downloaded files under OutputDir.
func (e *Engine) Run(ctx context.Context, seeds []string, cfg Config) (Summary, error) {
	summary := Summary{}
	if len(seeds) == 0 {
		return summary, errors.New("at least one seed URL is required")
	}

	outputRoot, err := resolveOutputRoot(cfg.OutputDir)
	if err != nil {
		return summary, err
	}

	maxDepth := cfg.MaxDepth
	if maxDepth < 0 {
		maxDepth = defaultMaxDepth
	}
	if maxDepth == 0 {
		maxDepth = defaultMaxDepth
	}

	maxPages := cfg.MaxPages
	if maxPages <= 0 {
		maxPages = defaultMaxPages
	}

	seedDomains := make([]string, 0, len(seeds))
	seedSchemes := make(map[string]struct{})
	queue := make([]crawlItem, 0, len(seeds))
	seen := make(map[string]struct{})
	failures := make([]CrawlError, 0)

	for _, raw := range seeds {
		canon, parsed, err := canonicalize(raw)
		if err != nil {
			return summary, fmt.Errorf("invalid seed URL %q: %w", raw, err)
		}
		domain := registrableDomain(parsed.Hostname())
		seedDomains = append(seedDomains, domain)
		seedSchemes[strings.ToLower(parsed.Scheme)] = struct{}{}
		if shouldExcludePath(parsed.EscapedPath(), cfg.ExcludeDirs) || shouldRejectURL(parsed, cfg.RejectPatterns) {
			continue
		}
		if _, ok := seen[canon]; ok {
			continue
		}
		seen[canon] = struct{}{}
		queue = append(queue, crawlItem{url: canon, depth: 0})
	}

	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return summary, err
		}

		item := queue[0]
		queue = queue[1:]
		summary.VisitedURLs++

		u, err := url.Parse(item.url)
		if err != nil {
			failures = append(failures, makeCrawlError(item.url, err))
			continue
		}

		if !matchesSeedDomain(u.Hostname(), seedDomains) {
			continue
		}

		if !cfg.AllowSchemeChange && !matchesSeedScheme(u.Scheme, seeds) {
			continue
		}
		if shouldExcludePath(u.EscapedPath(), cfg.ExcludeDirs) || shouldRejectURL(u, cfg.RejectPatterns) {
			continue
		}

		if cfg.RespectRobots {
			allowed, rErr := e.allowedByRobots(ctx, u, cfg.UserAgent)
			if rErr != nil {
				if cfg.StrictRobots {
					return summary, &RobotsError{URL: item.url, Cause: rErr}
				}
				// Non-fatal by default: proceed when robots fetch/parse fails.
			}
			if !allowed {
				continue
			}
		}

		saveRes, err := e.fetchAndSave(ctx, u, outputRoot, cfg.MaxTotalBytes, summary.TotalBytes, cfg.UserAgent, cfg.ExcludeDirs, cfg.RejectPatterns)
		if err != nil {
			if errors.Is(err, ErrMaxTotalBytesExceeded) {
				return summary, err
			}
			failures = append(failures, makeCrawlError(item.url, err))
			continue
		}
		if saveRes.skipped {
			continue
		}

		summary.ResourcesDownloaded++
		summary.TotalBytes += saveRes.bytes

		if !saveRes.isHTML {
			continue
		}

		summary.PagesVisited++
		if summary.PagesVisited > maxPages {
			return summary, ErrMaxPagesExceeded
		}

		links := saveRes.links
		if cfg.ConvertLinks {
			if cErr := convertHTMLFile(saveRes.path, saveRes.finalURL, outputRoot, seedDomains, seedSchemes, cfg); cErr != nil {
				failures = append(failures, makeCrawlError(item.url, fmt.Errorf("convert links: %w", cErr)))
			}
		}
		if item.depth >= maxDepth {
			continue
		}

		for _, link := range links {
			canon, parsedLink, cErr := canonicalize(link)
			if cErr != nil {
				continue
			}
			if !matchesSeedDomain(parsedLink.Hostname(), seedDomains) {
				continue
			}
			if !cfg.AllowSchemeChange {
				if _, ok := seedSchemes[strings.ToLower(parsedLink.Scheme)]; !ok {
					continue
				}
			}
			if shouldExcludePath(parsedLink.EscapedPath(), cfg.ExcludeDirs) || shouldRejectURL(parsedLink, cfg.RejectPatterns) {
				continue
			}
			if _, exists := seen[canon]; exists {
				continue
			}
			seen[canon] = struct{}{}
			queue = append(queue, crawlItem{url: canon, depth: item.depth + 1})
		}
	}

	if len(failures) > 0 {
		return summary, &AggregateError{Errors: failures}
	}
	return summary, nil
}

func makeCrawlError(rawURL string, err error) CrawlError {
	return CrawlError{
		URL:  rawURL,
		Code: errcode.Of(err),
		Err:  err,
	}
}

type saveResult struct {
	path     string
	bytes    int64
	isHTML   bool
	skipped  bool
	finalURL *url.URL
	links    []string
}

func (e *Engine) fetchAndSave(ctx context.Context, u *url.URL, outputRoot string, maxTotalBytes, currentTotal int64, userAgent string, excludeDirs, rejectPatterns []string) (saveResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return saveResult{}, err
	}
	if strings.TrimSpace(userAgent) != "" {
		req.Header.Set("User-Agent", userAgent)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return saveResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return saveResult{}, &HTTPStatusError{
			Code: resp.StatusCode,
			URL:  resp.Request.URL.String(),
		}
	}
	if shouldExcludePath(resp.Request.URL.EscapedPath(), excludeDirs) || shouldRejectURL(resp.Request.URL, rejectPatterns) {
		return saveResult{
			skipped:  true,
			finalURL: resp.Request.URL,
		}, nil
	}

	targetPath, err := mapURLToPath(outputRoot, resp.Request.URL)
	if err != nil {
		return saveResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return saveResult{}, fmt.Errorf("create mirror output dir: %w", err)
	}

	file, err := os.Create(targetPath)
	if err != nil {
		return saveResult{}, fmt.Errorf("create mirror output file: %w", err)
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	isHTML := strings.Contains(ct, "text/html")
	if isHTML {
		cw := &cappedWriter{
			dst:         file,
			maxTotal:    maxTotalBytes,
			currentBase: currentTotal,
		}
		tee := io.TeeReader(resp.Body, cw)
		links, parseErr := parser.ExtractLinksFromReader(resp.Request.URL.String(), tee)
		if closeErr := file.Close(); closeErr != nil && parseErr == nil {
			parseErr = closeErr
		}
		if parseErr != nil {
			_ = os.Remove(targetPath)
			if errors.Is(parseErr, ErrMaxTotalBytesExceeded) {
				return saveResult{}, ErrMaxTotalBytesExceeded
			}
			return saveResult{}, parseErr
		}
		return saveResult{
			path:     targetPath,
			bytes:    cw.written,
			isHTML:   true,
			finalURL: resp.Request.URL,
			links:    links,
		}, nil
	}

	written, err := copyWithCap(file, resp.Body, maxTotalBytes, currentTotal)
	if closeErr := file.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(targetPath)
		return saveResult{}, err
	}

	return saveResult{
		path:     targetPath,
		bytes:    written,
		isHTML:   false,
		finalURL: resp.Request.URL,
	}, nil
}

func copyWithCap(dst io.Writer, src io.Reader, maxTotalBytes, currentTotal int64) (int64, error) {
	buf := mirrorCopyBufferPool.Get().([]byte)
	defer mirrorCopyBufferPool.Put(buf)
	var total int64

	for {
		n, rErr := src.Read(buf)
		if n > 0 {
			if maxTotalBytes > 0 && currentTotal+total+int64(n) > maxTotalBytes {
				return total, ErrMaxTotalBytesExceeded
			}
			w, wErr := dst.Write(buf[:n])
			total += int64(w)
			if wErr != nil {
				return total, wErr
			}
			if w != n {
				return total, io.ErrShortWrite
			}
		}
		if rErr != nil {
			if errors.Is(rErr, io.EOF) {
				break
			}
			return total, rErr
		}
	}
	return total, nil
}

type cappedWriter struct {
	dst         io.Writer
	maxTotal    int64
	currentBase int64
	written     int64
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	if w.maxTotal > 0 && w.currentBase+w.written+int64(len(p)) > w.maxTotal {
		allowed := w.maxTotal - w.currentBase - w.written
		if allowed <= 0 {
			return 0, ErrMaxTotalBytesExceeded
		}
		n, err := w.dst.Write(p[:allowed])
		w.written += int64(n)
		if err != nil {
			return n, err
		}
		return n, ErrMaxTotalBytesExceeded
	}
	n, err := w.dst.Write(p)
	w.written += int64(n)
	return n, err
}

func mapURLToPath(root string, u *url.URL) (string, error) {
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", errors.New("url host is required")
	}

	p := u.EscapedPath()
	if p == "" || strings.HasSuffix(p, "/") {
		p = path.Join(p, "index.html")
	}

	if strings.HasPrefix(p, "/") {
		p = p[1:]
	}
	if p == "" {
		p = "index.html"
	}

	decoded, err := url.PathUnescape(p)
	if err != nil {
		return "", err
	}

	parts := strings.Split(decoded, "/")
	safe := make([]string, 0, len(parts)+1)
	safe = append(safe, host)
	for _, seg := range parts {
		seg = strings.TrimSpace(seg)
		if seg == "" || seg == "." || seg == ".." {
			continue
		}
		seg = mirrorPathSegmentReplacer.Replace(seg)
		safe = append(safe, seg)
	}
	if u.RawQuery != "" && len(safe) > 0 {
		last := safe[len(safe)-1]
		ext := filepath.Ext(last)
		base := strings.TrimSuffix(last, ext)
		last = fmt.Sprintf("%s_q_%x%s", base, shortHash(u.RawQuery), ext)
		safe[len(safe)-1] = last
	}

	candidate := filepath.Join(append([]string{root}, safe...)...)
	candidateAbs, err := filepath.Abs(filepath.Clean(candidate))
	if err != nil {
		return "", err
	}
	rootAbs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, candidateAbs)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("resolved mirror path escapes output root")
	}

	return candidateAbs, nil
}

func shortHash(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}

func resolveOutputRoot(outputDir string) (string, error) {
	dir := strings.TrimSpace(outputDir)
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	abs, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", err
	}
	return abs, nil
}

func canonicalize(raw string) (string, *url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", nil, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", nil, fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return "", nil, errors.New("missing host")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	if u.Path == "" {
		u.Path = "/"
	}
	if (u.Scheme == "http" && strings.HasSuffix(u.Host, ":80")) || (u.Scheme == "https" && strings.HasSuffix(u.Host, ":443")) {
		host := strings.Split(u.Host, ":")[0]
		u.Host = host
	}
	return u.String(), u, nil
}

func matchesSeedDomain(host string, seedDomains []string) bool {
	hostDomain := registrableDomain(host)
	for _, d := range seedDomains {
		if d == hostDomain {
			return true
		}
	}
	return false
}

func matchesSeedScheme(scheme string, seeds []string) bool {
	for _, seed := range seeds {
		u, err := url.Parse(seed)
		if err == nil && strings.EqualFold(u.Scheme, scheme) {
			return true
		}
	}
	return false
}

func registrableDomain(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return ""
	}
	d, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return host
	}
	return d
}

func (e *Engine) allowedByRobots(ctx context.Context, u *url.URL, userAgent string) (bool, error) {
	if userAgent == "" {
		userAgent = "*"
	}
	agent := strings.ToLower(userAgent)
	if agent != "*" {
		agent = "*"
	}

	key := strings.ToLower(u.Scheme + "://" + u.Host)

	e.robotsMu.Lock()
	rules, ok := e.robots[key]
	e.robotsMu.Unlock()
	if !ok {
		fetched, err := e.fetchRobots(ctx, u, agent)
		if err != nil {
			return true, err
		}
		e.robotsMu.Lock()
		e.robots[key] = fetched
		e.robotsMu.Unlock()
		rules = fetched
	}

	pathVal := u.EscapedPath()
	if pathVal == "" {
		pathVal = "/"
	}
	for _, dis := range rules.disallow {
		if dis == "" {
			continue
		}
		if strings.HasPrefix(pathVal, dis) {
			return false, nil
		}
	}
	return true, nil
}

func (e *Engine) fetchRobots(ctx context.Context, u *url.URL, agent string) (robotsRules, error) {
	robotsURL := u.Scheme + "://" + u.Host + "/robots.txt"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, robotsURL, nil)
	if err != nil {
		return robotsRules{}, err
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return robotsRules{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return robotsRules{}, nil
	}

	sc := bufio.NewScanner(resp.Body)
	rules := robotsRules{}
	applies := false
	for sc.Scan() {
		line := sc.Text()
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])

		switch key {
		case "user-agent":
			applies = strings.EqualFold(val, "*") || strings.EqualFold(val, agent)
		case "disallow":
			if applies {
				rules.disallow = append(rules.disallow, val)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return robotsRules{}, err
	}
	return rules, nil
}

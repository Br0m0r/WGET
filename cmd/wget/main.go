package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/pflag"

	"wget/internal/background"
	"wget/internal/cli"
	"wget/internal/concurrency"
	"wget/internal/downloader"
	"wget/internal/fs"
	"wget/internal/httpclient"
	"wget/internal/logger"
	"wget/internal/mirror"
	"wget/internal/progress"
	"wget/internal/ratelimiter"
)

const timestampLayout = "2006-01-02 15:04:05"

func main() {
	cfg, err := cli.ParseArgs(os.Args[1:])
	if err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			fmt.Print(cli.Usage())
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	log, err := logger.New(logger.Config{
		Debug:  cfg.Debug,
		Trace:  cfg.Trace,
		Format: cfg.LogFormat,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to initialize logger:", err)
		os.Exit(2)
	}

	if cfg.Background && !background.IsBackgroundChild() {
		exePath, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, "failed to resolve executable:", err)
			os.Exit(2)
		}

		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, "failed to resolve working directory:", err)
			os.Exit(2)
		}

		result, err := background.Start(background.Config{
			Executable: exePath,
			Args:       os.Args[1:],
			WorkingDir: wd,
			LogFile:    background.DefaultLogFile,
			PIDFile:    background.DefaultPIDFile,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}

		_ = result
		fmt.Printf("Output will be written to %q.\n", background.DefaultLogFile)
		return
	}

	if err := run(cfg, log); err != nil {
		log.Error(err, "command failed")
		os.Exit(1)
	}
}

func run(cfg cli.Config, log *logger.Logger) error {
	urls, err := resolveURLs(cfg)
	if err != nil {
		return err
	}
	if len(urls) == 0 {
		return errors.New("no URLs to process")
	}

	client := httpclient.New(httpclient.Config{
		Timeout:         30 * time.Second,
		MaxConnsPerHost: 50,
	})

	ctx := context.Background()
	limiter, err := newGlobalLimiter(cfg)
	if err != nil {
		return err
	}
	if cfg.Mirror {
		return runMirror(ctx, client, log, cfg, urls)
	}
	return runDownloads(ctx, client, log, cfg, urls, limiter)
}

func resolveURLs(cfg cli.Config) ([]string, error) {
	if cfg.InputFile == "" {
		return append([]string(nil), cfg.URLs...), nil
	}
	urls, err := concurrency.LoadURLsFromFile(cfg.InputFile)
	if err != nil {
		return nil, err
	}
	return urls, nil
}

func runMirror(ctx context.Context, client *http.Client, log *logger.Logger, cfg cli.Config, urls []string) error {
	engine := mirror.NewEngine(client)
	summary, err := engine.Run(ctx, urls, mirror.Config{
		OutputDir:         cfg.OutputDir,
		RejectPatterns:    cfg.RejectPatterns,
		ExcludeDirs:       cfg.ExcludeDirs,
		ConvertLinks:      cfg.ConvertLinks,
		AllowSchemeChange: true,
		RespectRobots:     true,
		StrictRobots:      cfg.StrictRobots,
	})
	if err != nil {
		return err
	}

	log.Info(
		"mirror completed",
		"pages_visited", summary.PagesVisited,
		"resources_downloaded", summary.ResourcesDownloaded,
		"total_bytes", summary.TotalBytes,
		"visited_urls", summary.VisitedURLs,
	)
	return nil
}

func runDownloads(ctx context.Context, client *http.Client, log *logger.Logger, cfg cli.Config, urls []string, limiter downloader.ByteLimiter) error {
	if len(urls) == 1 {
		return runSingleDownload(ctx, client, cfg, urls[0], limiter)
	}

	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}

	d := downloader.New(client)
	jobs := make([]concurrency.Job, 0, len(urls))
	preflightErrs := make([]error, 0)

	for i, rawURL := range urls {
		name := ""
		if len(urls) == 1 {
			name = cfg.OutputName
		}

		plan, planErr := fs.ResolveAndPrepare(fs.ResolveOptions{
			URL:        rawURL,
			OutputDir:  cfg.OutputDir,
			OutputName: name,
			WorkingDir: wd,
			Force:      cfg.Force,
		})
		if planErr != nil {
			preflightErrs = append(preflightErrs, fmt.Errorf("url %q: %w", rawURL, planErr))
			continue
		}

		jobs = append(jobs, concurrency.Job{
			ID: i + 1,
			Request: downloader.Request{
				URL:         rawURL,
				OutputPath:  plan.TargetPath,
				Timeout:     30 * time.Second,
				MaxRetries:  3,
				BackoffBase: 500 * time.Millisecond,
				BackoffMax:  5 * time.Second,
				Limiter:     limiter,
			},
		})
	}

	var runtimeErr error
	if len(jobs) > 0 {
		manager, newErr := concurrency.NewManager(d, concurrency.Config{Workers: 10})
		if newErr != nil {
			return newErr
		}

		summary, runErr := manager.Run(ctx, jobs)
		runtimeErr = runErr
		log.Info(
			"download summary",
			"total", summary.Total,
			"succeeded", summary.Succeeded,
			"failed", summary.Failed,
			"bytes_written", summary.BytesWritten,
		)
	}

	if len(preflightErrs) == 0 {
		return runtimeErr
	}

	joined := append([]error(nil), preflightErrs...)
	if runtimeErr != nil {
		joined = append(joined, runtimeErr)
	}
	return errors.Join(joined...)
}

func runSingleDownload(ctx context.Context, client *http.Client, cfg cli.Config, rawURL string, limiter downloader.ByteLimiter) error {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}

	plan, err := fs.ResolveAndPrepare(fs.ResolveOptions{
		URL:        rawURL,
		OutputDir:  cfg.OutputDir,
		OutputName: cfg.OutputName,
		WorkingDir: wd,
		Force:      cfg.Force,
	})
	if err != nil {
		return err
	}

	start := time.Now()
	fmt.Printf("start at %s\n", start.Format(timestampLayout))

	isTTY := stdoutIsTTY()
	var tracker *progress.Tracker
	var lastDownloaded int64
	responsePrinted := false

	printResponse := func(statusCode int, contentSize int64) {
		if responsePrinted {
			return
		}
		responsePrinted = true
		fmt.Printf("sending request, awaiting response... status %d %s\n", statusCode, http.StatusText(statusCode))
		if contentSize >= 0 {
			fmt.Printf("content size: %d [~%s]\n", contentSize, formatRoundedSize(contentSize))
		} else {
			fmt.Println("content size: unknown")
		}
		fmt.Printf("saving file to: %s\n", displayPath(plan.TargetPath, wd))
		tracker = progress.NewTracker(contentSize, progress.Options{
			IsTTY:          isTTY,
			UpdateInterval: 200 * time.Millisecond,
		})
	}

	d := downloader.New(client)
	res, err := d.Download(ctx, downloader.Request{
		URL:         rawURL,
		OutputPath:  plan.TargetPath,
		Timeout:     30 * time.Second,
		MaxRetries:  3,
		BackoffBase: 500 * time.Millisecond,
		BackoffMax:  5 * time.Second,
		Limiter:     limiter,
		OnResponse: func(statusCode int, contentSize int64) {
			printResponse(statusCode, contentSize)
		},
		OnProgress: func(downloaded int64, total int64) {
			if tracker == nil {
				return
			}
			delta := downloaded - lastDownloaded
			lastDownloaded = downloaded
			if delta > 0 {
				tracker.Add(delta)
			}
			now := time.Now()
			if !tracker.ShouldRender(now) {
				return
			}
			snap := tracker.SnapshotAt(now)
			if cfg.RateLimitBytes > 0 && snap.SpeedBPS > float64(cfg.RateLimitBytes) {
				snap.SpeedBPS = float64(cfg.RateLimitBytes)
			}
			line := tracker.Render(snap)
			if isTTY {
				fmt.Print(line)
				return
			}
			fmt.Println(line)
		},
	})

	if isTTY && tracker != nil {
		fmt.Println()
	}

	if err != nil {
		if !responsePrinted {
			if status, ok := statusFromError(err); ok {
				fmt.Printf("sending request, awaiting response... status %d %s\n", status, http.StatusText(status))
			}
			fmt.Printf("saving file to: %s\n", displayPath(plan.TargetPath, wd))
		}
		return err
	}

	if !responsePrinted {
		printResponse(res.StatusCode, res.ContentSize)
	}

	fmt.Printf("\nDownloaded [%s]\n", rawURL)
	fmt.Printf("finished at %s\n", res.EndTime.Format(timestampLayout))
	return nil
}

func statusFromError(err error) (int, bool) {
	var statusErr *downloader.HTTPStatusError
	if errors.As(err, &statusErr) {
		return statusErr.Code, true
	}
	var dlErr *downloader.DownloadError
	if errors.As(err, &dlErr) {
		for i := len(dlErr.Attempts) - 1; i >= 0; i-- {
			if dlErr.Attempts[i].StatusCode > 0 {
				return dlErr.Attempts[i].StatusCode, true
			}
		}
	}
	return 0, false
}

func stdoutIsTTY() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func formatRoundedSize(n int64) string {
	if n < 0 {
		return "unknown"
	}
	const mib = 1024 * 1024
	const gib = 1024 * mib

	if n >= gib {
		return fmt.Sprintf("%.2f GiB", float64(n)/float64(gib))
	}
	return fmt.Sprintf("%.2f MiB", float64(n)/float64(mib))
}

func displayPath(targetPath, wd string) string {
	rel, err := filepath.Rel(wd, targetPath)
	if err != nil {
		return targetPath
	}
	if rel == "." {
		return "./"
	}
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return targetPath
	}
	return "." + string(filepath.Separator) + rel
}

func newGlobalLimiter(cfg cli.Config) (downloader.ByteLimiter, error) {
	if cfg.RateLimitBytes <= 0 {
		return nil, nil
	}
	limiter, err := ratelimiter.New(ratelimiter.Config{
		BytesPerSec: cfg.RateLimitBytes,
		BurstBytes:  cfg.RateLimitBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("configure rate limiter: %w", err)
	}
	return limiter, nil
}

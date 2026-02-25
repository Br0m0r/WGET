package cli

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
)

var rateLimitPattern = regexp.MustCompile(`^\s*([0-9]+(?:\.[0-9]+)?)\s*([a-zA-Z/]+)\s*$`)
var splitFragmentPattern = regexp.MustCompile(`^\.[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Config holds CLI-level runtime options parsed from command flags.
type Config struct {
	Background     bool
	OutputName     string
	OutputDir      string
	Force          bool
	Debug          bool
	Trace          bool
	LogFormat      string
	RateLimitRaw   string
	RateLimitBytes int64
	InputFile      string
	Mirror         bool
	StrictRobots   bool
	RejectPatterns []string
	ExcludeDirs    []string
	ConvertLinks   bool
	URLs           []string
}

// ParseArgs parses and validates CLI arguments.
func ParseArgs(args []string) (Config, error) {
	args = normalizeArgs(args)
	fs, cfg := newFlagSet()
	buf := &bytes.Buffer{}
	fs.SetOutput(buf)

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			return *cfg, pflag.ErrHelp
		}
		return *cfg, fmt.Errorf("%v\n\n%s", err, renderUsage(fs))
	}

	cfg.URLs = fs.Args()
	cfg.OutputName = strings.TrimSpace(cfg.OutputName)
	cfg.OutputDir = strings.TrimSpace(cfg.OutputDir)
	cfg.InputFile = strings.TrimSpace(cfg.InputFile)
	cfg.RateLimitRaw = strings.TrimSpace(cfg.RateLimitRaw)
	cfg.LogFormat = strings.TrimSpace(strings.ToLower(cfg.LogFormat))
	if cfg.Trace {
		cfg.Debug = true
	}

	if cfg.OutputDir != "" {
		cfg.OutputDir = filepath.Clean(cfg.OutputDir)
	}

	if cfg.RateLimitRaw != "" {
		rate, err := ParseRateLimit(cfg.RateLimitRaw)
		if err != nil {
			return *cfg, fmt.Errorf("invalid --rate-limit: %w\n\n%s", err, renderUsage(fs))
		}
		cfg.RateLimitBytes = rate
	}

	if err := validateConfig(*cfg); err != nil {
		return *cfg, fmt.Errorf("%w\n\n%s", err, renderUsage(fs))
	}

	return *cfg, nil
}

func normalizeArgs(args []string) []string {
	if len(args) == 0 {
		return args
	}

	stringFlags := map[string]struct{}{
		"-O=":                 {},
		"--output-document=":  {},
		"-P=":                 {},
		"--directory-prefix=": {},
		"-i=":                 {},
		"--input-file=":       {},
		"--rate-limit=":       {},
	}

	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		joined := false
		for prefix := range stringFlags {
			if strings.HasPrefix(arg, prefix) && i+1 < len(args) {
				next := args[i+1]
				if splitFragmentPattern.MatchString(next) {
					out = append(out, arg+next)
					i++
					joined = true
				}
				break
			}
		}
		if joined {
			continue
		}
		out = append(out, arg)
	}
	return out
}

// Usage returns the CLI usage text.
func Usage() string {
	fs, _ := newFlagSet()
	return renderUsage(fs)
}

// ParseRateLimit parses values like 500k, 2m, 500KiB/s, and 2MiB/s.
func ParseRateLimit(value string) (int64, error) {
	match := rateLimitPattern.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return 0, fmt.Errorf("expected formats like 500k, 2m, 500KiB/s, or 2MiB/s")
	}

	numValue, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid numeric value %q", match[1])
	}
	if numValue <= 0 {
		return 0, fmt.Errorf("value must be greater than 0")
	}

	unit := strings.ToLower(match[2])
	multiplier, ok := map[string]float64{
		"k":     1024,
		"m":     1024 * 1024,
		"kib/s": 1024,
		"mib/s": 1024 * 1024,
		"kb/s":  1024,
		"mb/s":  1024 * 1024,
		"kibps": 1024,
		"mibps": 1024 * 1024,
		"kbps":  1024,
		"mbps":  1024 * 1024,
	}[unit]
	if !ok {
		return 0, fmt.Errorf("unsupported unit %q", match[2])
	}

	bytesPerSecond := numValue * multiplier
	if bytesPerSecond > float64(math.MaxInt64) {
		return 0, fmt.Errorf("value is too large")
	}

	return int64(bytesPerSecond), nil
}

func newFlagSet() (*pflag.FlagSet, *Config) {
	cfg := &Config{}
	fs := pflag.NewFlagSet("wget", pflag.ContinueOnError)
	fs.SortFlags = false

	fs.BoolVarP(&cfg.Background, "background", "B", false, "run in background and redirect logs")
	fs.StringVarP(&cfg.OutputName, "output-document", "O", "", "write output to file name (works with single URL only)")
	fs.StringVarP(&cfg.OutputDir, "directory-prefix", "P", "", "save files to this directory")
	fs.BoolVar(&cfg.Force, "force", false, "overwrite existing target files")
	fs.BoolVar(&cfg.Debug, "debug", false, "enable debug logging")
	fs.BoolVar(&cfg.Trace, "trace", false, "enable trace logging (implies --debug)")
	fs.StringVar(&cfg.LogFormat, "log-format", "human", "log format: human|json")
	fs.StringVar(&cfg.RateLimitRaw, "rate-limit", "", "limit rate (500k, 2m, 500KiB/s, 2MiB/s)")
	fs.StringVarP(&cfg.InputFile, "input-file", "i", "", "download URLs found in local file")
	fs.BoolVar(&cfg.Mirror, "mirror", false, "enable recursive mirroring mode")
	fs.BoolVar(&cfg.StrictRobots, "strict-robots", false, "fail mirror run if robots.txt cannot be fetched or parsed")
	fs.StringSliceVarP(&cfg.RejectPatterns, "reject", "R", nil, "reject file types/patterns (comma-separated)")
	fs.StringSliceVarP(&cfg.ExcludeDirs, "exclude-directories", "X", nil, "exclude directories in mirror mode (comma-separated)")
	fs.BoolVar(&cfg.ConvertLinks, "convert-links", false, "convert links for local browsing (mirror mode)")

	return fs, cfg
}

func renderUsage(fs *pflag.FlagSet) string {
	var b strings.Builder
	b.WriteString("Usage:\n")
	b.WriteString("  wget [flags] URL\n")
	b.WriteString("  wget [flags] -i FILE\n\n")
	b.WriteString("Flags:\n")
	b.WriteString(fs.FlagUsages())
	return b.String()
}

func validateConfig(cfg Config) error {
	if cfg.InputFile == "" && len(cfg.URLs) == 0 {
		return errors.New("a URL or -i/--input-file is required")
	}

	if cfg.InputFile != "" && len(cfg.URLs) > 0 {
		return errors.New("cannot mix positional URLs with -i/--input-file")
	}

	if cfg.ConvertLinks && !cfg.Mirror {
		return errors.New("--convert-links requires --mirror")
	}

	if len(cfg.RejectPatterns) > 0 && !cfg.Mirror {
		return errors.New("-R/--reject requires --mirror")
	}

	if len(cfg.ExcludeDirs) > 0 && !cfg.Mirror {
		return errors.New("-X/--exclude-directories requires --mirror")
	}

	if cfg.StrictRobots && !cfg.Mirror {
		return errors.New("--strict-robots requires --mirror")
	}

	if cfg.OutputName != "" && cfg.Mirror {
		return errors.New("-O/--output-document cannot be combined with --mirror")
	}

	if cfg.OutputName != "" && cfg.InputFile != "" {
		return errors.New("-O/--output-document cannot be combined with -i/--input-file")
	}

	if cfg.OutputName != "" && len(cfg.URLs) > 1 {
		return errors.New("-O/--output-document supports exactly one URL")
	}

	if cfg.OutputName != "" && filepath.Base(cfg.OutputName) != cfg.OutputName {
		return errors.New("-O/--output-document must be a filename, not a path")
	}

	if cfg.LogFormat != "" && cfg.LogFormat != "human" && cfg.LogFormat != "json" {
		return errors.New("--log-format must be either human or json")
	}

	for _, rawURL := range cfg.URLs {
		parsed, err := url.Parse(rawURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("invalid URL %q", rawURL)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return fmt.Errorf("unsupported URL scheme %q", parsed.Scheme)
		}
	}

	return nil
}

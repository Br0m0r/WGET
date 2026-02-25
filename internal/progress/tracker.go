package progress

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

const defaultUpdateInterval = 200 * time.Millisecond

// Options controls tracker behavior and rendering style.
type Options struct {
	IsTTY          bool
	UpdateInterval time.Duration
}

// Snapshot captures progress state at a point in time.
type Snapshot struct {
	Downloaded int64
	Total      int64
	Percent    float64
	SpeedBPS   float64
	ETA        time.Duration
	Complete   bool
}

// Tracker computes progress metrics and renders user-facing output.
type Tracker struct {
	mu           sync.Mutex
	total        int64
	downloaded   int64
	start        time.Time
	lastRendered time.Time
	isTTY        bool
	interval     time.Duration
}

// NewTracker creates a progress tracker for a transfer.
func NewTracker(total int64, opts Options) *Tracker {
	interval := opts.UpdateInterval
	if interval <= 0 {
		interval = defaultUpdateInterval
	}
	return &Tracker{
		total:    total,
		start:    time.Now(),
		isTTY:    opts.IsTTY,
		interval: interval,
	}
}

// Add records transferred bytes.
func (t *Tracker) Add(n int64) {
	if n <= 0 {
		return
	}
	t.mu.Lock()
	t.downloaded += n
	t.mu.Unlock()
}

// Snapshot returns computed progress at the current time.
func (t *Tracker) Snapshot() Snapshot {
	return t.SnapshotAt(time.Now())
}

// SnapshotAt returns computed progress at a specific time (used for testing).
func (t *Tracker) SnapshotAt(now time.Time) Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	return computeSnapshot(t.start, now, t.downloaded, t.total)
}

// ShouldRender reports whether output should be refreshed at the current time.
func (t *Tracker) ShouldRender(now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.lastRendered.IsZero() || now.Sub(t.lastRendered) >= t.interval {
		t.lastRendered = now
		return true
	}
	return false
}

// Render returns one progress line for the provided snapshot.
func (t *Tracker) Render(s Snapshot) string {
	if t.isTTY {
		return renderTTY(s)
	}
	return renderPlain(s)
}

func computeSnapshot(start, now time.Time, downloaded, total int64) Snapshot {
	if downloaded < 0 {
		downloaded = 0
	}

	var percent float64
	if total > 0 {
		percent = float64(downloaded) * 100 / float64(total)
		if percent > 100 {
			percent = 100
		}
	}

	elapsed := now.Sub(start).Seconds()
	speed := 0.0
	if elapsed > 0 {
		speed = float64(downloaded) / elapsed
	}

	eta := time.Duration(-1)
	if total > 0 && speed > 0 && downloaded < total {
		remaining := float64(total - downloaded)
		eta = time.Duration(remaining/speed) * time.Second
	}
	complete := total > 0 && downloaded >= total
	if complete {
		eta = 0
	}

	return Snapshot{
		Downloaded: downloaded,
		Total:      total,
		Percent:    percent,
		SpeedBPS:   speed,
		ETA:        eta,
		Complete:   complete,
	}
}

func renderTTY(s Snapshot) string {
	const width = 30
	filled := int(math.Round((s.Percent / 100) * width))
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}

	bar := strings.Repeat("#", filled) + strings.Repeat("-", width-filled)
	return fmt.Sprintf(
		"\r[%s] %6.2f%%  %s/%s  %s  ETA %s",
		bar,
		s.Percent,
		formatBytes(s.Downloaded),
		formatTotal(s.Total),
		formatRate(s.SpeedBPS),
		formatETA(s.ETA),
	)
}

func renderPlain(s Snapshot) string {
	return fmt.Sprintf(
		"progress downloaded=%s total=%s percent=%.2f speed=%s eta=%s",
		formatBytes(s.Downloaded),
		formatTotal(s.Total),
		s.Percent,
		formatRate(s.SpeedBPS),
		formatETA(s.ETA),
	)
}

func formatTotal(total int64) string {
	if total <= 0 {
		return "unknown"
	}
	return formatBytes(total)
}

func formatETA(d time.Duration) string {
	if d < 0 {
		return "unknown"
	}
	return d.Round(time.Second).String()
}

func formatRate(bps float64) string {
	if bps <= 0 {
		return "0 B/s"
	}
	return fmt.Sprintf("%s/s", formatBytes(int64(bps)))
}

func formatBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	value := float64(n)
	unit := ""
	for _, u := range units {
		value /= 1024
		unit = u
		if value < 1024 {
			break
		}
	}
	return fmt.Sprintf("%.2f %s", value, unit)
}

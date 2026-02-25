package progress

import (
	"strings"
	"testing"
	"time"
)

func TestSnapshot_PercentageAccuracy(t *testing.T) {
	start := time.Unix(100, 0)

	s := computeSnapshot(start, start.Add(2*time.Second), 250, 1000)
	if s.Percent != 25 {
		t.Fatalf("expected 25%%, got %.2f%%", s.Percent)
	}
	if s.Complete {
		t.Fatal("expected incomplete snapshot")
	}

	s = computeSnapshot(start, start.Add(2*time.Second), 1000, 1000)
	if s.Percent != 100 {
		t.Fatalf("expected 100%%, got %.2f%%", s.Percent)
	}
	if !s.Complete {
		t.Fatal("expected complete snapshot at total bytes")
	}

	s = computeSnapshot(start, start.Add(2*time.Second), 1400, 1000)
	if s.Percent != 100 {
		t.Fatalf("expected capped 100%%, got %.2f%%", s.Percent)
	}
}

func TestSnapshot_SpeedCalculationCorrectness(t *testing.T) {
	start := time.Unix(200, 0)
	s := computeSnapshot(start, start.Add(4*time.Second), 2000, 4000)

	if s.SpeedBPS != 500 {
		t.Fatalf("expected 500 B/s, got %.2f", s.SpeedBPS)
	}
	if s.ETA != 4*time.Second {
		t.Fatalf("expected ETA 4s, got %s", s.ETA)
	}
}

func TestTracker_ShouldRenderInterval(t *testing.T) {
	tr := NewTracker(1000, Options{IsTTY: true, UpdateInterval: 200 * time.Millisecond})
	now := time.Unix(300, 0)

	if !tr.ShouldRender(now) {
		t.Fatal("expected first render to be allowed")
	}
	if tr.ShouldRender(now.Add(150 * time.Millisecond)) {
		t.Fatal("expected second render to be throttled")
	}
	if !tr.ShouldRender(now.Add(250 * time.Millisecond)) {
		t.Fatal("expected render after interval")
	}
}

func TestRender_Modes(t *testing.T) {
	s := Snapshot{
		Downloaded: 512,
		Total:      1024,
		Percent:    50,
		SpeedBPS:   256,
		ETA:        2 * time.Second,
	}

	tty := NewTracker(1024, Options{IsTTY: true})
	outTTY := tty.Render(s)
	if !strings.Contains(outTTY, "[") || !strings.Contains(outTTY, "50.00%") {
		t.Fatalf("unexpected tty render output: %q", outTTY)
	}

	plain := NewTracker(1024, Options{IsTTY: false})
	outPlain := plain.Render(s)
	if !strings.Contains(outPlain, "progress downloaded=") {
		t.Fatalf("unexpected plain render output: %q", outPlain)
	}
}

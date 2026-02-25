package httpclient

import (
	"net/http"
	"testing"
	"time"
)

func TestNew_DefaultConfig(t *testing.T) {
	client := New(Config{})
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.Timeout != 30*time.Second {
		t.Fatalf("unexpected default timeout: got %s want %s", client.Timeout, 30*time.Second)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", client.Transport)
	}

	if transport.MaxIdleConns != 100 {
		t.Fatalf("unexpected MaxIdleConns: got %d want %d", transport.MaxIdleConns, 100)
	}
	if transport.MaxIdleConnsPerHost != 10 {
		t.Fatalf("unexpected MaxIdleConnsPerHost: got %d want %d", transport.MaxIdleConnsPerHost, 10)
	}
	if transport.MaxConnsPerHost != 50 {
		t.Fatalf("unexpected MaxConnsPerHost: got %d want %d", transport.MaxConnsPerHost, 50)
	}
	if transport.Proxy == nil {
		t.Fatal("expected proxy resolver from environment to be configured")
	}
}

func TestNew_CustomConfig(t *testing.T) {
	cfg := Config{
		Timeout:             15 * time.Second,
		MaxIdleConns:        25,
		MaxIdleConnsPerHost: 7,
		MaxConnsPerHost:     9,
	}

	client := New(cfg)
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", client.Transport)
	}

	if client.Timeout != cfg.Timeout {
		t.Fatalf("unexpected timeout: got %s want %s", client.Timeout, cfg.Timeout)
	}
	if transport.MaxIdleConns != cfg.MaxIdleConns {
		t.Fatalf("unexpected MaxIdleConns: got %d want %d", transport.MaxIdleConns, cfg.MaxIdleConns)
	}
	if transport.MaxIdleConnsPerHost != cfg.MaxIdleConnsPerHost {
		t.Fatalf("unexpected MaxIdleConnsPerHost: got %d want %d", transport.MaxIdleConnsPerHost, cfg.MaxIdleConnsPerHost)
	}
	if transport.MaxConnsPerHost != cfg.MaxConnsPerHost {
		t.Fatalf("unexpected MaxConnsPerHost: got %d want %d", transport.MaxConnsPerHost, cfg.MaxConnsPerHost)
	}
}

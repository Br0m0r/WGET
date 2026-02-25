package errcode

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"
)

type codedErr struct {
	code string
}

func (e codedErr) Error() string     { return "coded" }
func (e codedErr) ErrorCode() string { return e.code }

func TestOf(t *testing.T) {
	t.Run("coded error wins", func(t *testing.T) {
		if got := Of(codedErr{code: CodeHTTP5XX}); got != CodeHTTP5XX {
			t.Fatalf("unexpected code: got %s want %s", got, CodeHTTP5XX)
		}
	})

	t.Run("context timeout", func(t *testing.T) {
		if got := Of(context.DeadlineExceeded); got != CodeNetworkTime {
			t.Fatalf("unexpected code: got %s want %s", got, CodeNetworkTime)
		}
	})

	t.Run("context canceled", func(t *testing.T) {
		if got := Of(context.Canceled); got != CodeCanceled {
			t.Fatalf("unexpected code: got %s want %s", got, CodeCanceled)
		}
	})

	t.Run("permission error", func(t *testing.T) {
		err := &os.PathError{Op: "open", Path: "/tmp/x", Err: os.ErrPermission}
		if got := Of(err); got != CodeFSPermission {
			t.Fatalf("unexpected code: got %s want %s", got, CodeFSPermission)
		}
	})

	t.Run("path error", func(t *testing.T) {
		err := &os.PathError{Op: "open", Path: "/tmp/x", Err: errors.New("boom")}
		if got := Of(err); got != CodeFSError {
			t.Fatalf("unexpected code: got %s want %s", got, CodeFSError)
		}
	})

	t.Run("network timeout", func(t *testing.T) {
		err := &net.DNSError{IsTimeout: true}
		if got := Of(err); got != CodeNetworkTime {
			t.Fatalf("unexpected code: got %s want %s", got, CodeNetworkTime)
		}
	})
}

func TestHTTPStatus(t *testing.T) {
	cases := map[int]string{
		408: CodeHTTP408,
		429: CodeHTTP429,
		500: CodeHTTP5XX,
		503: CodeHTTP5XX,
		404: CodeHTTP4XX,
		302: CodeUnknown,
	}
	for status, want := range cases {
		if got := HTTPStatus(status); got != want {
			t.Fatalf("status %d: got %s want %s", status, got, want)
		}
	}
}

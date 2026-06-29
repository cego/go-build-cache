package cache

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

type countingTransport struct {
	calls atomic.Int32
}

func (t *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls.Add(1)
	return nil, errors.New("connection refused")
}

// Once the server is unreachable, the circuit breaker must stop contacting it:
// the transport is hit at most once across many operations, and operations
// keep succeeding from local disk.
func TestHTTPClientCircuitBreaker(t *testing.T) {
	tr := &countingTransport{}
	c := &HTTPClient{
		BaseURL: "http://127.0.0.1:0",
		Disk:    &DiskCache{Dir: t.TempDir()},
		HTTP:    &http.Client{Transport: tr},
	}

	for i := 0; i < 50; i++ {
		if _, _, err := c.Get(context.Background(), strings.Repeat("a", 64)); err != nil {
			t.Fatalf("Get %d returned error, want graceful miss: %v", i, err)
		}
	}

	if got := tr.calls.Load(); got != 1 {
		t.Fatalf("transport called %d times after server went down, want 1 (circuit breaker open)", got)
	}
}

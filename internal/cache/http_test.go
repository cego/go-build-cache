package cache

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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

func TestHTTPClientDefaultTimeout(t *testing.T) {
	if (&HTTPClient{}).client().Timeout <= 0 {
		t.Fatal("default HTTP client has no timeout")
	}
}

// A server that stalls must trip the breaker instead of hanging the build.
func TestHTTPClientStall(t *testing.T) {
	outputID := strings.Repeat("b", 64)
	cases := map[string]func(w http.ResponseWriter, r *http.Request, stall <-chan struct{}){
		"before headers": func(w http.ResponseWriter, r *http.Request, stall <-chan struct{}) {
			<-stall
		},
		"action body": func(w http.ResponseWriter, r *http.Request, stall <-chan struct{}) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"outputID":`))
			w.(http.Flusher).Flush()
			<-stall
		},
		"output body": func(w http.ResponseWriter, r *http.Request, stall <-chan struct{}) {
			if strings.HasPrefix(r.URL.Path, "/action/") {
				_, _ = w.Write([]byte(`{"outputID":"` + outputID + `","size":10}`))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("12345"))
			w.(http.Flusher).Flush()
			<-stall
		},
	}

	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			stall := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				handler(w, r, stall)
			}))
			// Close blocks until handlers return, so release them first.
			t.Cleanup(srv.Close)
			t.Cleanup(func() { close(stall) })

			c := &HTTPClient{
				BaseURL: srv.URL,
				Disk:    &DiskCache{Dir: t.TempDir()},
				HTTP:    &http.Client{Timeout: 50 * time.Millisecond},
			}

			for i := 0; i < 2; i++ {
				out, _, err := c.Get(context.Background(), strings.Repeat("a", 64))
				if err != nil || out != "" {
					t.Fatalf("Get %d = (%q, %v), want miss", i, out, err)
				}
			}
			callsAfter := calls.Load()

			if _, _, err := c.Get(context.Background(), strings.Repeat("c", 64)); err != nil {
				t.Fatalf("Get after trip: %v", err)
			}
			if got := calls.Load(); got != callsAfter {
				t.Fatalf("server called %d times after trip, want %d", got, callsAfter)
			}
		})
	}
}

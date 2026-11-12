package cmd

import (
	"testing"
	"time"

	"github.com/cego/go-build-cache/internal/cache"
)

func TestServerTimeout(t *testing.T) {
	for env, want := range map[string]time.Duration{
		"":      cache.DefaultTimeout,
		"2m":    2 * time.Minute,
		"bogus": cache.DefaultTimeout,
		"0s":    cache.DefaultTimeout,
	} {
		t.Setenv("GO_BUILD_CACHE_TIMEOUT", env)
		if got := serverTimeout(); got != want {
			t.Errorf("GO_BUILD_CACHE_TIMEOUT=%q: got %s, want %s", env, got, want)
		}
	}
}

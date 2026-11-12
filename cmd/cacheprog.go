package cmd

import (
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cego/go-build-cache/internal/cache"
	"github.com/cego/go-build-cache/internal/cacheprog"
	"github.com/spf13/cobra"
)

var cacheprogCmd = &cobra.Command{
	Use:   "cacheprog",
	Short: "GOCACHEPROG child process for the go toolchain (talks to the server, falls back to local disk)",
	RunE:  runCacheprog,
}

var cacheprogFlags struct {
	cacheDir    string
	cacheServer string
	gatewayPort int
	accessToken string
	verbose     bool
}

func init() {
	rootCmd.AddCommand(cacheprogCmd)
	f := cacheprogCmd.Flags()
	f.StringVar(&cacheprogFlags.cacheDir, "cache-dir", "", "local disk cache directory; empty means OS default")
	f.StringVar(&cacheprogFlags.cacheServer, "cache-server", "", "cache server HTTP prefix (scheme and authority)")
	f.IntVar(&cacheprogFlags.gatewayPort, "gateway-addr-port", 0, "if non-zero, probe an HTTP server on this port on the machine's gateway IP; fall back to local disk if unreachable")
	f.StringVar(&cacheprogFlags.accessToken, "access-token", "", "optional bearer token for the cache server")
	f.BoolVar(&cacheprogFlags.verbose, "verbose", false, "be verbose")
}

func runCacheprog(_ *cobra.Command, _ []string) error {
	dir, err := resolveCacheDir(cacheprogFlags.cacheDir)
	if err != nil {
		return err
	}

	disk := &cache.DiskCache{Dir: dir}
	proc := &cacheprog.Process{
		Close: func() error { return nil },
		Get:   disk.Get,
		Put:   disk.Put,
	}

	serverBase := cacheprogFlags.cacheServer
	if serverBase == "" && cacheprogFlags.gatewayPort != 0 {
		if gw, ok := gatewayIP(); ok {
			serverBase = "http://" + net.JoinHostPort(gw, fmt.Sprint(cacheprogFlags.gatewayPort))
		} else {
			log.Printf("go-build-cache: no gateway IP found, using local disk")
		}
	}

	if serverBase != "" {
		if host, err := hostPort(serverBase); err != nil {
			log.Printf("go-build-cache: invalid cache server %q, using local disk: %v", serverBase, err)
		} else if c, err := net.DialTimeout("tcp", host, time.Second/2); err != nil {
			log.Printf("go-build-cache: cache server %s unreachable, using local disk only: %v", serverBase, err)
		} else {
			_ = c.Close()
			hc := &cache.HTTPClient{BaseURL: serverBase, Disk: disk, HTTP: &http.Client{Timeout: serverTimeout()}}
			proc.Get = hc.Get
			proc.Put = hc.Put
		}
	}

	return proc.Run()
}

func serverTimeout() time.Duration {
	v := os.Getenv("GO_BUILD_CACHE_TIMEOUT")
	if v == "" {
		return cache.DefaultTimeout
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		log.Printf("go-build-cache: invalid GO_BUILD_CACHE_TIMEOUT %q, using %s", v, cache.DefaultTimeout)
		return cache.DefaultTimeout
	}
	return d
}

func hostPort(base string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	return net.JoinHostPort(u.Hostname(), port), nil
}

func gatewayIP() (string, bool) {
	data, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(data), "\n")[1:] {
		f := strings.Fields(line)
		if len(f) < 3 || f[1] != "00000000" {
			continue
		}
		b, err := hex.DecodeString(f[2])
		if err != nil || len(b) != 4 {
			continue
		}
		return fmt.Sprintf("%d.%d.%d.%d", b[3], b[2], b[1], b[0]), true
	}
	return "", false
}

func resolveCacheDir(explicit string) (string, error) {
	if explicit != "" {
		return explicit, os.MkdirAll(explicit, 0o755)
	}
	if d, err := os.UserCacheDir(); err == nil {
		p := filepath.Join(d, "go-build-cache")
		if os.MkdirAll(p, 0o755) == nil {
			return p, nil
		}
	}
	p := filepath.Join(os.TempDir(), "go-build-cache")
	return p, os.MkdirAll(p, 0o755)
}

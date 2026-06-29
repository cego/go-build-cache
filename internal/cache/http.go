package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
)

// HTTPClient is a GOCACHEPROG backend that talks to a go-build-cache server
// over HTTP, keeping a local disk copy. HTTP failures are non-fatal: gets
// degrade to a miss and puts keep the local disk result, so a flaky or down
// server never fails the build. The first transport error trips a circuit
// breaker: it is logged once, after which the client serves from local disk
// only and stops contacting the server, so a build on a machine without
// access never floods the log.
type HTTPClient struct {
	BaseURL string
	Disk    *DiskCache
	HTTP    *http.Client

	down    atomic.Bool
	logOnce sync.Once
}

func (c *HTTPClient) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *HTTPClient) trip(err error) {
	c.down.Store(true)
	c.logOnce.Do(func() {
		log.Printf("go-build-cache: cache server %s unreachable, using local disk only: %v", c.BaseURL, err)
	})
}

func (c *HTTPClient) Get(ctx context.Context, actionID string) (outputID, diskPath string, err error) {
	if outputID, diskPath, err = c.Disk.Get(ctx, actionID); err == nil && outputID != "" {
		return outputID, diskPath, nil
	}
	if c.down.Load() {
		return "", "", nil
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/action/"+actionID, nil)
	res, err := c.client().Do(req)
	if err != nil {
		c.trip(err)
		return "", "", nil
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return "", "", nil
	}
	if res.StatusCode != http.StatusOK {
		log.Printf("go-build-cache: GET action %s: %s", actionID, res.Status)
		return "", "", nil
	}

	var av ActionValue
	if err := json.NewDecoder(res.Body).Decode(&av); err != nil {
		return "", "", nil
	}

	var body io.Reader = bytes.NewReader(nil)
	if av.Size > 0 {
		oreq, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/output/"+av.OutputID, nil)
		ores, err := c.client().Do(oreq)
		if err != nil {
			c.trip(err)
			return "", "", nil
		}
		defer ores.Body.Close()
		if ores.StatusCode != http.StatusOK {
			return "", "", nil
		}
		body = ores.Body
	}

	diskPath, err = c.Disk.Put(ctx, actionID, av.OutputID, av.Size, body)
	if err != nil {
		return "", "", err
	}
	return av.OutputID, diskPath, nil
}

func (c *HTTPClient) Put(ctx context.Context, actionID, outputID string, size int64, body io.Reader) (string, error) {
	buf, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}

	diskPath, err := c.Disk.Put(ctx, actionID, outputID, size, bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	if c.down.Load() {
		return diskPath, nil
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodPut, c.BaseURL+"/"+actionID+"/"+outputID, bytes.NewReader(buf))
	req.ContentLength = size
	res, err := c.client().Do(req)
	if err != nil {
		c.trip(err)
		return diskPath, nil
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		log.Printf("go-build-cache: PUT %s/%s: %s", actionID, outputID, res.Status)
	}
	return diskPath, nil
}

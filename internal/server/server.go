package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/bradfitz/go-tool-cache/cachers"
)

// Cache is the storage the server exposes over HTTP.
type Cache interface {
	Get(ctx context.Context, actionID string) (outputID, diskPath string, err error)
	Put(ctx context.Context, actionID, outputID string, size int64, body io.Reader) (diskPath string, err error)
	OutputFilename(outputID string) string
}

type Server struct {
	Cache   Cache
	Verbose bool
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPut:
		s.handlePut(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/action/"):
		s.handleGetAction(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/output/"):
		s.handleGetOutput(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/":
		_, _ = io.WriteString(w, "go-build-cache")
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (s *Server) handleGetAction(w http.ResponseWriter, r *http.Request) {
	actionID, ok := hexSuffix(r.URL.Path, "/action/")
	if !ok {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	outputID, diskPath, err := s.Cache.Get(r.Context(), actionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if outputID == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	fi, err := os.Stat(diskPath)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(&cachers.ActionValue{OutputID: outputID, Size: fi.Size()})
}

func (s *Server) handleGetOutput(w http.ResponseWriter, r *http.Request) {
	outputID, ok := hexSuffix(r.URL.Path, "/output/")
	if !ok {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, s.Cache.OutputFilename(outputID))
}

func (s *Server) handlePut(w http.ResponseWriter, r *http.Request) {
	actionID, outputID, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if !ok || !validHex(actionID) || !validHex(outputID) {
		http.Error(w, "bad URI", http.StatusBadRequest)
		return
	}
	if r.ContentLength < 0 {
		http.Error(w, "missing Content-Length", http.StatusBadRequest)
		return
	}
	if _, err := s.Cache.Put(r.Context(), actionID, outputID, r.ContentLength, r.Body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func hexSuffix(path, prefix string) (string, bool) {
	suffix := strings.TrimPrefix(path, prefix)
	if !validHex(suffix) {
		return "", false
	}
	return suffix, true
}

func validHex(x string) bool {
	if len(x) < 4 || len(x) > 1000 || len(x)%2 == 1 {
		return false
	}
	for i := range x {
		b := x[i]
		if (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') {
			continue
		}
		return false
	}
	return true
}

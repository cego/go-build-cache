package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// ActionValue is the JSON the server returns for a GET /action request.
type ActionValue struct {
	OutputID string `json:"outputID"`
	Size     int64  `json:"size"`
}

type indexEntry struct {
	OutputID  string `json:"o"`
	Size      int64  `json:"n"`
	TimeNanos int64  `json:"t"`
}

// DiskCache is a content-addressed cache on local disk.
type DiskCache struct {
	Dir string
}

func (d *DiskCache) actionFilename(actionID string) string {
	return filepath.Join(d.Dir, actionID[:2], "a-"+actionID)
}

func (d *DiskCache) OutputFilename(outputID string) string {
	if !validHex(outputID) {
		return ""
	}
	return filepath.Join(d.Dir, outputID[:2], "o-"+outputID)
}

func (d *DiskCache) Get(_ context.Context, actionID string) (outputID, diskPath string, err error) {
	if !validHex(actionID) {
		return "", "", fmt.Errorf("invalid actionID %q", actionID)
	}
	data, err := os.ReadFile(d.actionFilename(actionID))
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", nil
		}
		return "", "", err
	}
	var ie indexEntry
	if err := json.Unmarshal(data, &ie); err != nil || !validHex(ie.OutputID) {
		return "", "", nil
	}
	return ie.OutputID, d.OutputFilename(ie.OutputID), nil
}

func (d *DiskCache) Put(_ context.Context, actionID, outputID string, size int64, body io.Reader) (string, error) {
	if !validHex(actionID) || !validHex(outputID) {
		return "", fmt.Errorf("actionID and outputID must be hex")
	}
	outputFile := d.OutputFilename(outputID)
	if err := os.MkdirAll(filepath.Dir(outputFile), 0o755); err != nil {
		return "", err
	}
	actionFile := d.actionFilename(actionID)
	if err := os.MkdirAll(filepath.Dir(actionFile), 0o755); err != nil {
		return "", err
	}
	if _, err := writeFileAtomic(outputFile, body, size); err != nil {
		return "", err
	}
	ie, err := json.Marshal(indexEntry{OutputID: outputID, Size: size, TimeNanos: time.Now().UnixNano()})
	if err != nil {
		return "", err
	}
	if _, err := writeFileAtomic(actionFile, bytes.NewReader(ie), -1); err != nil {
		return "", err
	}
	return outputFile, nil
}

func writeFileAtomic(path string, r io.Reader, size int64) (int64, error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmp.Name())

	n, err := io.Copy(tmp, r)
	if err != nil {
		_ = tmp.Close()
		return n, err
	}
	if err := tmp.Close(); err != nil {
		return n, err
	}
	if size >= 0 && n != size {
		return n, fmt.Errorf("wrote %d bytes, expected %d", n, size)
	}
	return n, os.Rename(tmp.Name(), path)
}

func validHex(x string) bool {
	if len(x) < 4 || len(x) > 1000 {
		return false
	}
	for _, b := range x {
		if (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') {
			continue
		}
		return false
	}
	return true
}

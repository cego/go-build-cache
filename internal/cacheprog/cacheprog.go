// Package cacheprog implements cmd/go's GOCACHEPROG JSON protocol over
// stdin/stdout so a higher-level cache can be plugged in via Get/Put/Close.
package cacheprog

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

type Cmd string

const (
	cmdPut   = Cmd("put")
	cmdClose = Cmd("close")
)

type Request struct {
	ID       int64
	Command  Cmd
	ActionID []byte    `json:",omitempty"`
	OutputID []byte    `json:",omitempty"`
	ObjectID []byte    `json:",omitempty"`
	Body     io.Reader `json:"-"`
	BodySize int64     `json:",omitempty"`
}

type Response struct {
	ID            int64
	Err           string `json:",omitempty"`
	KnownCommands []Cmd  `json:",omitempty"`
	Miss          bool   `json:",omitempty"`
	OutputID      []byte `json:",omitempty"`
	Size          int64  `json:",omitempty"`
	TimeNanos     int64  `json:",omitempty"`
	DiskPath      string `json:",omitempty"`
}

// Process answers cmd/go cache requests using the provided funcs.
type Process struct {
	Get   func(ctx context.Context, actionID string) (outputID, diskPath string, err error)
	Put   func(ctx context.Context, actionID, outputID string, size int64, r io.Reader) (diskPath string, err error)
	Close func() error
}

func (p *Process) Run() error {
	jd := json.NewDecoder(bufio.NewReader(os.Stdin))
	bw := bufio.NewWriter(os.Stdout)
	je := json.NewEncoder(bw)

	var caps []Cmd
	if p.Get != nil {
		caps = append(caps, "get")
	}
	if p.Put != nil {
		caps = append(caps, "put")
	}
	if p.Close != nil {
		caps = append(caps, "close")
	}
	if err := je.Encode(&Response{KnownCommands: caps}); err != nil {
		return err
	}
	if err := bw.Flush(); err != nil {
		return err
	}

	var wmu sync.Mutex
	ctx := context.Background()

	for {
		var req Request
		if err := jd.Decode(&req); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if len(req.OutputID) == 0 && len(req.ObjectID) != 0 {
			req.OutputID = req.ObjectID
		}
		if req.Command == cmdPut && req.BodySize > 0 {
			var body []byte
			if err := jd.Decode(&body); err != nil {
				return err
			}
			if int64(len(body)) != req.BodySize {
				return fmt.Errorf("got %d body bytes, declared %d", len(body), req.BodySize)
			}
			req.Body = bytes.NewReader(body)
		}
		go func() {
			res := &Response{ID: req.ID}
			if err := p.handle(ctx, &req, res); err != nil {
				res.Err = err.Error()
			}
			wmu.Lock()
			defer wmu.Unlock()
			_ = je.Encode(res)
			_ = bw.Flush()
		}()
	}
}

func (p *Process) handle(ctx context.Context, req *Request, res *Response) error {
	switch req.Command {
	case cmdClose:
		if p.Close != nil {
			return p.Close()
		}
		return nil
	case "get":
		return p.handleGet(ctx, req, res)
	case cmdPut:
		return p.handlePut(ctx, req, res)
	default:
		return errors.New("unknown command")
	}
}

func (p *Process) handleGet(ctx context.Context, req *Request, res *Response) error {
	if p.Get == nil {
		res.Miss = true
		return nil
	}
	outputID, diskPath, err := p.Get(ctx, fmt.Sprintf("%x", req.ActionID))
	if err != nil {
		return err
	}
	if outputID == "" {
		res.Miss = true
		return nil
	}
	res.OutputID, err = hex.DecodeString(outputID)
	if err != nil {
		return fmt.Errorf("invalid outputID: %w", err)
	}
	fi, err := os.Stat(diskPath)
	if err != nil {
		if os.IsNotExist(err) {
			res.Miss = true
			return nil
		}
		return err
	}
	if !fi.Mode().IsRegular() {
		return errors.New("not a regular file")
	}
	res.Size = fi.Size()
	res.TimeNanos = fi.ModTime().UnixNano()
	res.DiskPath = diskPath
	return nil
}

func (p *Process) handlePut(ctx context.Context, req *Request, res *Response) error {
	if p.Put == nil {
		if req.Body != nil {
			_, _ = io.Copy(io.Discard, req.Body)
		}
		return nil
	}
	body := req.Body
	if body == nil {
		body = bytes.NewReader(nil)
	}
	diskPath, err := p.Put(ctx, fmt.Sprintf("%x", req.ActionID), fmt.Sprintf("%x", req.OutputID), req.BodySize, body)
	if err != nil {
		return err
	}
	res.DiskPath = diskPath
	return nil
}

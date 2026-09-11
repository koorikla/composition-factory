package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ResilientTransport provides a stream/stdio Transport that handles malformed
// or non-JSON input in compliance with JSON-RPC 2.0 without terminating the
// session prematurely.
type ResilientTransport struct {
	r io.Reader
	w io.Writer
}

// NewStdioTransport returns an sdk.Transport connected to os.Stdin and os.Stdout
// that survives invalid, malformed, or batch JSON-RPC input on stdin.
func NewStdioTransport() sdk.Transport {
	return NewStreamTransport(os.Stdin, os.Stdout)
}

// NewStreamTransport returns an sdk.Transport for the given reader and writer.
func NewStreamTransport(r io.Reader, w io.Writer) sdk.Transport {
	return &ResilientTransport{r: r, w: w}
}

// Connect implements sdk.Transport.
func (t *ResilientTransport) Connect(ctx context.Context) (sdk.Connection, error) {
	return newResilientConn(t.r, t.w), nil
}

type resilientConn struct {
	r io.Reader
	w io.Writer

	mu sync.Mutex // guards writes to w

	incoming  chan msgOrErr
	closed    chan struct{}
	closeOnce sync.Once
	closeErr  error
}

type msgOrErr struct {
	msg jsonrpc.Message
	err error
}

func newResilientConn(r io.Reader, w io.Writer) *resilientConn {
	c := &resilientConn{
		r:        r,
		w:        w,
		incoming: make(chan msgOrErr),
		closed:   make(chan struct{}),
	}
	go c.readLoop()
	return c
}

func (c *resilientConn) readLoop() {
	br := bufio.NewReader(c.r)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			select {
			case c.incoming <- msgOrErr{err: err}:
			case <-c.closed:
			}
			return
		}

		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			if err != nil {
				select {
				case c.incoming <- msgOrErr{err: err}:
				case <-c.closed:
				}
				return
			}
			continue
		}

		// Support optional Content-Length framing (LSP/header-framed streams).
		if bytes.HasPrefix(bytes.ToLower(trimmed), []byte("content-length:")) {
			parts := bytes.SplitN(trimmed, []byte(":"), 2)
			if len(parts) == 2 {
				length, parseErr := strconv.Atoi(string(bytes.TrimSpace(parts[1])))
				if parseErr == nil && length > 0 {
					for {
						hdr, hErr := br.ReadBytes('\n')
						if hErr != nil {
							break
						}
						if len(bytes.TrimSpace(hdr)) == 0 {
							break
						}
					}
					payload := make([]byte, length)
					if _, readErr := io.ReadFull(br, payload); readErr == nil {
						trimmed = bytes.TrimSpace(payload)
					}
				}
			}
		}

		if len(trimmed) == 0 {
			if err != nil {
				select {
				case c.incoming <- msgOrErr{err: err}:
				case <-c.closed:
				}
				return
			}
			continue
		}

		// Reject JSON-RPC batching (spec 2025-06-18+ does not support batching).
		if trimmed[0] == '[' {
			c.writeErrorResponse(nil, jsonrpc.CodeInvalidRequest, "Invalid Request: batching is not supported")
			if err != nil {
				select {
				case c.incoming <- msgOrErr{err: err}:
				case <-c.closed:
				}
				return
			}
			continue
		}

		// Validate raw JSON syntax.
		var raw any
		if jsonErr := json.Unmarshal(trimmed, &raw); jsonErr != nil {
			c.writeErrorResponse(nil, jsonrpc.CodeParseError, "Parse error")
			if err != nil {
				select {
				case c.incoming <- msgOrErr{err: err}:
				case <-c.closed:
				}
				return
			}
			continue
		}

		// Decode into typed JSON-RPC message.
		msg, decErr := jsonrpc.DecodeMessage(trimmed)
		if decErr != nil {
			id := extractID(trimmed)
			c.writeErrorResponse(id, jsonrpc.CodeInvalidRequest, "Invalid Request")
			if err != nil {
				select {
				case c.incoming <- msgOrErr{err: err}:
				case <-c.closed:
				}
				return
			}
			continue
		}

		select {
		case c.incoming <- msgOrErr{msg: msg}:
		case <-c.closed:
			return
		}

		if err != nil {
			select {
			case c.incoming <- msgOrErr{err: err}:
			case <-c.closed:
			}
			return
		}
	}
}

func extractID(data []byte) json.RawMessage {
	var probe struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(data, &probe); err == nil && len(probe.ID) > 0 {
		var val any
		if json.Unmarshal(probe.ID, &val) == nil {
			switch val.(type) {
			case string, float64:
				return probe.ID
			}
		}
	}
	return nil
}

func (c *resilientConn) writeErrorResponse(id json.RawMessage, code int64, message string) {
	idStr := "null"
	if len(id) > 0 {
		idStr = string(id)
	}
	resp := fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":{"code":%d,"message":%q}}`+"\n", idStr, code, message)

	c.mu.Lock()
	_, _ = c.w.Write([]byte(resp))
	c.mu.Unlock()

	fmt.Fprintf(os.Stderr, "cf: mcp: %s (code %d)\n", message, code)
}

func (c *resilientConn) Read(ctx context.Context) (jsonrpc.Message, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closed:
		return nil, io.EOF
	case item := <-c.incoming:
		return item.msg, item.err
	}
}

func (c *resilientConn) Write(ctx context.Context, msg jsonrpc.Message) error {
	data, err := jsonrpc.EncodeMessage(msg)
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}
	data = append(data, '\n')

	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.w.Write(data)
	return err
}

func (c *resilientConn) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
		if closer, ok := c.r.(io.Closer); ok {
			if closer != os.Stdin {
				_ = closer.Close()
			}
		}
		if closer, ok := c.w.(io.Closer); ok {
			if closer != os.Stdout && closer != os.Stderr {
				_ = closer.Close()
			}
		}
	})
	return nil
}

func (c *resilientConn) SessionID() string {
	return ""
}

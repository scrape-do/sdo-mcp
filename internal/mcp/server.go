// SPDX-License-Identifier: Apache-2.0

// Package mcp is a minimal Model Context Protocol server (stdio, newline-framed
// JSON-RPC 2.0) bundled in the sdo CLI as `sdo mcp serve`. It exposes the
// scraping surface as MCP tools, reusing the sdo Go SDK.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	sdo "github.com/scrape-do/sdo-go"
)

const serverName = "sdo-mcp"

// latestProtocolVersion is offered when the client asks for a version this
// server does not speak; the client then decides whether to continue.
const latestProtocolVersion = "2025-06-18"

// negotiateProtocol echoes the client's protocol version when this server
// supports it, and otherwise answers with the latest one it does.
func negotiateProtocol(requested string) string {
	switch requested {
	case "2025-06-18", "2025-03-26", "2024-11-05":
		return requested
	}
	return latestProtocolVersion
}

// JSON-RPC 2.0 error codes. codeServerBusy is from the implementation-defined
// server error range.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeServerBusy     = -32000
)

// Scraper is the subset of *sdo.Client the server needs (injectable for tests).
type Scraper interface {
	Scrape(ctx context.Context, c *sdo.ScrapeConfig) (*sdo.Result, error)
	// PluginResult calls a plugin and returns its response with status and
	// headers, so the tool can report the cost like scrape does.
	PluginResult(ctx context.Context, route string, params map[string]any) (*sdo.Result, error)
	// Redact masks the client's token and credential shapes; every tool
	// result passes through it before it reaches the model.
	Redact(text string) string
}

// Request / Response are JSON-RPC 2.0 envelopes.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 reply. ID is not omitempty: the spec requires
// `"id": null` when the request could not be parsed, and omitting the member
// entirely makes the reply invalid.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func result(id json.RawMessage, res any) *Response {
	return &Response{JSONRPC: "2.0", ID: id, Result: res}
}

func rpcError(id json.RawMessage, code int, msg string) *Response {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return &Response{JSONRPC: "2.0", ID: id, Error: &RPCError{Code: code, Message: msg}}
}

// parseMessage validates one line as a JSON-RPC 2.0 request object. It returns
// the request, or the error reply to send instead.
func parseMessage(line []byte) (*Request, *Response) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(line, &fields); err != nil {
		if !json.Valid(line) {
			return nil, rpcError(nil, codeParseError, "parse error")
		}
		return nil, rpcError(nil, codeInvalidRequest,
			"invalid request: expected a single JSON-RPC request object (batches are not supported)")
	}
	id, hasID := fields["id"]
	if hasID && !validID(id) {
		return nil, rpcError(nil, codeInvalidRequest, "invalid request: id must be a string or a number")
	}
	var version, method string
	if json.Unmarshal(fields["jsonrpc"], &version) != nil || version != "2.0" {
		return nil, rpcError(id, codeInvalidRequest, `invalid request: "jsonrpc" must be "2.0"`)
	}
	if json.Unmarshal(fields["method"], &method) != nil || method == "" {
		return nil, rpcError(id, codeInvalidRequest, `invalid request: "method" must be a non-empty string`)
	}
	params := fields["params"]
	if len(params) > 0 && params[0] != '{' && params[0] != '[' {
		return nil, rpcError(id, codeInvalidRequest, `invalid request: "params" must be an object or an array`)
	}
	return &Request{JSONRPC: version, ID: id, Method: method, Params: params}, nil
}

// validID accepts the id shapes MCP allows: a string or a number (not null).
func validID(id json.RawMessage) bool {
	c := id[0]
	return c == '"' || c == '-' || ('0' <= c && c <= '9')
}

// Handle processes one valid request and returns a response, or nil for
// notifications.
func Handle(ctx context.Context, s Scraper, req *Request) *Response {
	if len(req.ID) == 0 {
		return nil // notification (e.g. notifications/initialized): no response
	}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p) // absent or malformed: offer the latest
		return result(req.ID, map[string]any{
			"protocolVersion": negotiateProtocol(p.ProtocolVersion),
			"serverInfo":      map[string]any{"name": serverName, "version": serverVersion},
			"capabilities":    map[string]any{"tools": map[string]any{}},
		})
	case "ping":
		return result(req.ID, map[string]any{})
	case "tools/list":
		return result(req.ID, map[string]any{"tools": toolDefs()})
	case "tools/call":
		return handleToolCall(ctx, s, req)
	default:
		return rpcError(req.ID, codeMethodNotFound, "method not found: "+req.Method)
	}
}

// maxConcurrentCalls bounds how many tool calls run at once; more wait their
// turn (or their cancellation) without holding up the rest of the protocol.
const maxConcurrentCalls = 4

// maxPendingCalls bounds how many tool calls may be running or waiting for a
// slot at once. A call beyond it is refused with codeServerBusy instead of
// parking yet another goroutine (and its request) in memory.
const maxPendingCalls = 64

// maxLineBytes is the longest request line Serve accepts. A longer line is
// answered with an invalid-request error (id null) and skipped; the server
// keeps serving.
const maxLineBytes = 16 * 1024 * 1024

// Serve runs the stdio JSON-RPC loop until the input is closed or ctx is
// done, then waits for the tool calls still running and returns. A done ctx
// is a clean shutdown (nil), even while a read from in is still blocked: the
// loop does not wait for the client to close its end.
//
// Tool calls run concurrently, at most maxConcurrentCalls at a time and
// maxPendingCalls running or waiting, so one slow scrape does not hold up a
// ping or an unrelated call; every other request is answered in order, as it
// is read. A notifications/cancelled naming a running (or waiting) call
// cancels its context — the request to the gateway is abandoned — and no
// reply is sent for it. Replies are written whole, one per line, each with
// its request's id.
func Serve(ctx context.Context, s Scraper, in io.Reader, out io.Writer) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	srv := &server{s: s, enc: json.NewEncoder(out), running: map[string]*inflight{},
		slots: make(chan struct{}, maxConcurrentCalls)}
	srv.enc.SetEscapeHTML(false)
	lines, readErr := readLines(ctx, in)
loop:
	for srv.writeErr() == nil {
		var l inputLine
		select {
		case <-ctx.Done():
			break loop
		case got, ok := <-lines:
			if !ok {
				break loop
			}
			l = got
		}
		if l.tooLong {
			srv.write(rpcError(nil, codeInvalidRequest, "invalid request: the message is longer than the 16 MiB line limit"))
			continue
		}
		line := bytes.TrimSpace(l.data)
		if len(line) == 0 {
			continue
		}
		req, resp := parseMessage(line)
		switch {
		case req == nil:
			srv.write(resp)
		case req.Method == "notifications/cancelled":
			srv.cancel(req.Params)
		case req.Method == "tools/call" && len(req.ID) > 0:
			srv.start(ctx, req)
		default:
			srv.write(Handle(ctx, s, req))
		}
	}
	shutdown := ctx.Err() != nil
	srv.calls.Wait()
	if err := srv.writeErr(); err != nil {
		return err
	}
	if shutdown {
		return nil
	}
	return *readErr
}

// inputLine is one newline-framed message, or the marker for one that
// exceeded maxLineBytes (its bytes are discarded).
type inputLine struct {
	data    []byte
	tooLong bool
}

// readLines reads in on its own goroutine, so a blocked read never keeps
// Serve from seeing ctx end. The channel is closed at EOF or on a read error,
// which is stored in *err (nil at EOF) before the close. The goroutine stops
// handing over lines once ctx is done; a read already blocked in in stays
// blocked until in returns (os.Stdin, at process exit).
func readLines(ctx context.Context, in io.Reader) (<-chan inputLine, *error) {
	ch := make(chan inputLine)
	errp := new(error)
	go func() {
		defer close(ch)
		r := bufio.NewReaderSize(in, 64*1024)
		for {
			l, err := readLine(r)
			if len(l.data) > 0 || l.tooLong {
				select {
				case ch <- l:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					*errp = err
				}
				return
			}
		}
	}()
	return ch, errp
}

// readLine returns the next line without its newline. Past maxLineBytes it
// stops buffering, reads on to the newline and reports the line as tooLong.
func readLine(r *bufio.Reader) (inputLine, error) {
	var l inputLine
	for {
		chunk, err := r.ReadSlice('\n')
		if err == nil {
			chunk = chunk[:len(chunk)-1] // the newline
		}
		if !l.tooLong {
			if len(l.data)+len(chunk) > maxLineBytes {
				l.tooLong, l.data = true, nil
			} else {
				l.data = append(l.data, chunk...)
			}
		}
		if err != bufio.ErrBufferFull {
			return l, err
		}
	}
}

type server struct {
	s     Scraper
	slots chan struct{} // one token per running tool call
	calls sync.WaitGroup

	mu      sync.Mutex
	enc     *json.Encoder
	err     error                // the first write error; the loop stops on it
	running map[string]*inflight // tool calls by request id
}

// inflight is one tool call that has not been answered yet.
type inflight struct {
	cancel    context.CancelFunc
	cancelled bool // by the client: its reply is dropped
}

// write sends one reply; a nil reply is nothing to send.
func (srv *server) write(resp *Response) {
	if resp == nil {
		return
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.err == nil {
		srv.err = srv.enc.Encode(resp)
	}
}

func (srv *server) writeErr() error {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	return srv.err
}

// start runs a tool call in its own goroutine, under its own context.
func (srv *server) start(ctx context.Context, req *Request) {
	key := string(req.ID)
	srv.mu.Lock()
	if _, busy := srv.running[key]; busy {
		srv.mu.Unlock()
		srv.write(rpcError(req.ID, codeInvalidRequest, "invalid request: id "+key+" is already in use by a running call"))
		return
	}
	if len(srv.running) >= maxPendingCalls {
		srv.mu.Unlock()
		srv.write(rpcError(req.ID, codeServerBusy, fmt.Sprintf(
			"server busy: %d tool calls are already running or waiting; retry when one finishes", maxPendingCalls)))
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	c := &inflight{cancel: cancel}
	srv.running[key] = c
	srv.mu.Unlock()

	srv.calls.Add(1)
	go func() {
		defer srv.calls.Done()
		defer cancel()
		var resp *Response
		select {
		case srv.slots <- struct{}{}:
			if ctx.Err() == nil { // select picks at random when both are ready
				resp = Handle(ctx, srv.s, req)
			}
			<-srv.slots
		case <-ctx.Done(): // cancelled while waiting for a slot
		}
		srv.mu.Lock()
		delete(srv.running, key)
		drop := c.cancelled
		srv.mu.Unlock()
		if !drop {
			srv.write(resp)
		}
	}()
}

// cancel handles notifications/cancelled: {"requestId": <id>, "reason": ...}.
// An id that is not running (finished, unknown, malformed) is ignored, as the
// protocol asks.
func (srv *server) cancel(params json.RawMessage) {
	var p struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if json.Unmarshal(params, &p) != nil || len(p.RequestID) == 0 {
		return
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if c, ok := srv.running[string(p.RequestID)]; ok {
		c.cancelled = true
		c.cancel()
	}
}

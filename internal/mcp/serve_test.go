// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	sdo "github.com/scrape-do/sdo-go"
)

// slowScraper blocks every scrape until release is closed or the call's
// context is cancelled, reporting both.
type slowScraper struct {
	started, cancelled chan string
	release            chan struct{}
}

func newSlowScraper() *slowScraper {
	return &slowScraper{started: make(chan string, 16), cancelled: make(chan string, 16), release: make(chan struct{})}
}

func (f *slowScraper) Scrape(ctx context.Context, c *sdo.ScrapeConfig) (*sdo.Result, error) {
	f.started <- c.URL
	select {
	case <-f.release:
		return &sdo.Result{StatusCode: 200, URL: c.URL}, nil
	case <-ctx.Done():
		f.cancelled <- c.URL
		return nil, ctx.Err()
	}
}

func (f *slowScraper) PluginResult(context.Context, string, map[string]any) (*sdo.Result, error) {
	return nil, errors.New("not used")
}

func (f *slowScraper) Redact(s string) string { return s }

func (f *slowScraper) Jobs() *sdo.AsyncJobs { return nil }

// session drives Serve over pipes, one line at a time.
type session struct {
	t    *testing.T
	in   *io.PipeWriter
	dec  *json.Decoder
	done chan error
}

func startSession(t *testing.T, s Scraper) *session {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	ss := &session{t: t, in: inW, dec: json.NewDecoder(outR), done: make(chan error, 1)}
	go func() {
		err := Serve(context.Background(), s, inR, outW)
		outW.Close()
		ss.done <- err
	}()
	return ss
}

func (ss *session) send(line string) {
	ss.t.Helper()
	if _, err := io.WriteString(ss.in, line+"\n"); err != nil {
		ss.t.Fatalf("send: %v", err)
	}
}

func (ss *session) scrape(id, url string) {
	ss.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"method":"tools/call","params":{"name":"scrape","arguments":{"url":%q}}}`, id, url))
}

// next reads one reply and returns its raw id and whether it is an error.
func (ss *session) next() (id string, isErr bool) {
	ss.t.Helper()
	var r struct {
		ID    json.RawMessage `json:"id"`
		Error *RPCError       `json:"error"`
	}
	if err := ss.dec.Decode(&r); err != nil {
		ss.t.Fatalf("read reply: %v", err)
	}
	return string(r.ID), r.Error != nil
}

// close ends the input, waits for Serve, and fails on any reply left over.
func (ss *session) close() {
	ss.t.Helper()
	ss.in.Close()
	var extra any
	if err := ss.dec.Decode(&extra); err != io.EOF {
		ss.t.Errorf("unexpected reply after the last one: %v (%v)", extra, err)
	}
	if err := <-ss.done; err != nil {
		ss.t.Errorf("Serve: %v", err)
	}
}

func recv(t *testing.T, ch chan string) string {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting")
		return ""
	}
}

// A slow tool call does not hold up a ping; each reply carries its own id.
func TestSlowCallDoesNotBlockOtherRequests(t *testing.T) {
	f := newSlowScraper()
	ss := startSession(t, f)
	ss.scrape(`"slow"`, "https://slow.example")
	recv(t, f.started)
	ss.send(`{"jsonrpc":"2.0","id":7,"method":"ping"}`)
	if id, _ := ss.next(); id != "7" {
		t.Fatalf("first reply id = %s, want the ping's 7", id)
	}
	close(f.release)
	if id, isErr := ss.next(); id != `"slow"` || isErr {
		t.Errorf("second reply id = %s (error %v), want the call's", id, isErr)
	}
	ss.close()
}

// notifications/cancelled cancels the call's context (the gateway request is
// abandoned) and suppresses its reply.
func TestCancelledCallStopsAndSendsNoReply(t *testing.T) {
	f := newSlowScraper()
	ss := startSession(t, f)
	ss.scrape("2", "https://slow.example")
	recv(t, f.started)
	ss.send(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":2,"reason":"user"}}`)
	if got := recv(t, f.cancelled); got != "https://slow.example" {
		t.Errorf("cancelled %q", got)
	}
	ss.send(`{"jsonrpc":"2.0","id":3,"method":"ping"}`)
	if id, _ := ss.next(); id != "3" {
		t.Errorf("reply id = %s, want only the ping's", id)
	}
	// Cancelling something not running is ignored.
	ss.send(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":99}}`)
	ss.send(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{}}`)
	ss.send(`{"jsonrpc":"2.0","method":"notifications/cancelled"}`)
	ss.close()
}

// At most maxConcurrentCalls run at once; a call waiting for its turn can be
// cancelled too, and never reaches the scraper.
func TestConcurrencyIsBoundedAndWaitingCallsCancel(t *testing.T) {
	f := newSlowScraper()
	ss := startSession(t, f)
	waiting := map[string]bool{}
	for i := 1; i <= maxConcurrentCalls+1; i++ {
		ss.scrape(fmt.Sprint(i), fmt.Sprintf("https://e.com/%d", i))
		waiting[fmt.Sprint(i)] = true
	}
	for i := 0; i < maxConcurrentCalls; i++ { // which ones get a slot first is up to the scheduler
		delete(waiting, strings.TrimPrefix(recv(t, f.started), "https://e.com/"))
	}
	ss.send(`{"jsonrpc":"2.0","id":100,"method":"ping"}`)
	if id, _ := ss.next(); id != "100" {
		t.Fatalf("reply id = %s", id)
	}
	select {
	case u := <-f.started:
		t.Fatalf("call %s started beyond the limit", u)
	case <-time.After(50 * time.Millisecond):
	}
	if len(waiting) != 1 {
		t.Fatalf("waiting calls: %v", waiting)
	}
	var queued string
	for id := range waiting {
		queued = id
	}
	ss.send(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":` + queued + `}}`)
	ss.send(`{"jsonrpc":"2.0","id":101,"method":"ping"}`) // lines are handled in order: the cancel is done
	if id, _ := ss.next(); id != "101" {
		t.Fatalf("reply id = %s", id)
	}
	close(f.release)
	seen := map[string]bool{}
	for i := 0; i < maxConcurrentCalls; i++ {
		id, _ := ss.next()
		seen[id] = true
	}
	ss.close()
	if len(seen) != maxConcurrentCalls || seen[queued] {
		t.Errorf("replies for %v (cancelled %s)", seen, queued)
	}
	if len(f.started) != 0 {
		t.Error("the cancelled waiting call reached the scraper")
	}
}

// Two running calls cannot share an id: cancellation names calls by id.
func TestDuplicateRunningIDIsRejected(t *testing.T) {
	f := newSlowScraper()
	ss := startSession(t, f)
	ss.scrape("1", "https://e.com/a")
	recv(t, f.started)
	ss.scrape("1", "https://e.com/b")
	if id, isErr := ss.next(); id != "1" || !isErr {
		t.Errorf("reply %s error=%v, want an error for the duplicate", id, isErr)
	}
	close(f.release)
	if id, isErr := ss.next(); id != "1" || isErr {
		t.Errorf("reply %s error=%v, want the first call's result", id, isErr)
	}
	ss.close()
}

// A failed write from a tool call is returned by Serve.
func TestServeReturnsACallsWriteError(t *testing.T) {
	f := newSlowScraper()
	close(f.release)
	in := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"scrape","arguments":{"url":"https://e.com"}}}` + "\n"
	if err := Serve(context.Background(), f, strings.NewReader(in), brokenWriter{}); err == nil || err.Error() != "pipe closed" {
		t.Errorf("want the write error, got %v", err)
	}
}

// A done context stops Serve even though the client never closes its input:
// SIGINT/SIGTERM must not wait for stdin to reach EOF.
func TestServeStopsWhenContextEndsWithInputOpen(t *testing.T) {
	inR, inW := io.Pipe()
	defer inW.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, nil, inR, io.Discard) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("a cancelled Serve is a clean shutdown, got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve kept blocking on open input after its context ended")
	}
}

// A running call is cancelled when the server context ends, and Serve still
// returns while the input stays open.
func TestServeShutdownCancelsRunningCalls(t *testing.T) {
	f := newSlowScraper()
	inR, inW := io.Pipe()
	defer inW.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, f, inR, io.Discard) }()
	io.WriteString(inW, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"scrape","arguments":{"url":"https://e.com/a"}}}`+"\n")
	recv(t, f.started)
	cancel()
	recv(t, f.cancelled)
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return after its context ended")
	}
}

// A line over maxLineBytes is answered with an invalid-request error (id
// null) and skipped; the next request is still served.
func TestOverlongLineIsRejectedAndServingContinues(t *testing.T) {
	in := strings.Repeat("x", maxLineBytes+1) + "\n" + `{"jsonrpc":"2.0","id":7,"method":"ping"}` + "\n"
	var out strings.Builder
	if err := Serve(context.Background(), nil, strings.NewReader(in), &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	dec := json.NewDecoder(strings.NewReader(out.String()))
	var first, second Response
	if err := dec.Decode(&first); err != nil || first.Error == nil || first.Error.Code != codeInvalidRequest || string(first.ID) != "null" {
		t.Errorf("overlong line: want an invalid-request error with id null, got %+v (%v)", first, err)
	}
	if err := dec.Decode(&second); err != nil || second.Error != nil || string(second.ID) != "7" {
		t.Errorf("the request after the overlong line was not served: %+v (%v)", second, err)
	}
}

// A line of exactly maxLineBytes is still read whole (and here, being no
// JSON, is a parse error rather than an overlong one).
func TestLineAtTheLimitIsRead(t *testing.T) {
	in := strings.Repeat("x", maxLineBytes)
	var out strings.Builder
	if err := Serve(context.Background(), nil, strings.NewReader(in), &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var r Response
	if err := json.Unmarshal([]byte(out.String()), &r); err != nil || r.Error == nil || r.Error.Code != codeParseError {
		t.Errorf("want a parse error, got %q (%v)", out.String(), err)
	}
}

// Past maxPendingCalls running or waiting calls, a new call is refused with
// codeServerBusy instead of queueing another goroutine; the others still run.
func TestPendingCallsAreBounded(t *testing.T) {
	f := newSlowScraper()
	f.started = make(chan string, maxPendingCalls)
	ss := startSession(t, f)
	for i := 1; i <= maxPendingCalls; i++ {
		ss.scrape(fmt.Sprint(i), fmt.Sprintf("https://e.com/%d", i))
	}
	for i := 0; i < maxConcurrentCalls; i++ {
		recv(t, f.started)
	}
	ss.send(`{"jsonrpc":"2.0","id":"over","method":"tools/call","params":{"name":"scrape","arguments":{"url":"https://e.com/x"}}}`)
	var busy Response
	if err := ss.dec.Decode(&busy); err != nil || busy.Error == nil || busy.Error.Code != codeServerBusy || string(busy.ID) != `"over"` {
		t.Fatalf("want a server-busy error for the call over the limit, got %+v (%v)", busy, err)
	}
	close(f.release)
	for i := 0; i < maxPendingCalls; i++ {
		if id, isErr := ss.next(); isErr {
			t.Errorf("call %s failed", id)
		}
	}
	ss.close()
}

// endless is input that never ends: every read is another line.
type endless struct{}

func (endless) Read(p []byte) (int, error) { return copy(p, "{}\n"), nil }

// Once ctx is done, readLines stops handing over lines and closes its
// channel, even while the input still has more to give.
func TestReadLinesStopsWhenContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	lines, errp := readLines(ctx, endless{})
	if l := <-lines; string(l.data) != "{}" {
		t.Fatalf("first line = %q", l.data)
	}
	cancel()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case _, ok := <-lines:
			if !ok {
				if *errp != nil {
					t.Errorf("a cancelled read is not an error, got %v", *errp)
				}
				return
			}
		case <-timeout:
			t.Fatal("readLines kept reading after its context ended")
		}
	}
}

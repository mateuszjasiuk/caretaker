package ctl

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// serveOne answers the next request on srv with reply and returns what it got.
func serveOne(t *testing.T, srv *Server, reply Response) <-chan Request {
	t.Helper()
	got := make(chan Request, 1)
	go func() {
		p := <-srv.Requests()
		got <- p.Req
		p.Reply(reply)
	}()
	return got
}

// shortDir makes a state directory short enough for a socket path: the
// per-test temp dirs are too long for one on macOS.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "ctt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func listenAt(t *testing.T, dir string) *Server {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", dir)
	srv, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	return srv
}

func TestRoundTrip(t *testing.T) {
	srv := listenAt(t, shortDir(t))
	got := serveOne(t, srv, Response{OK: true, Message: "sent"})

	resp, err := Call(srv.Path(), Request{Op: OpSend, Worktree: "r/a", Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Message != "sent" {
		t.Fatalf("message = %q", resp.Message)
	}
	req := <-got
	if req.Op != OpSend || req.Worktree != "r/a" || req.Text != "hello" {
		t.Fatalf("server saw %+v", req)
	}
}

func TestErrorResponseIsError(t *testing.T) {
	srv := listenAt(t, shortDir(t))
	serveOne(t, srv, Response{Error: "no such worktree"})

	_, err := Call(srv.Path(), Request{Op: OpSummary, Worktree: "r/x"})
	if err == nil || err.Error() != "no such worktree" {
		t.Fatalf("err = %v", err)
	}
}

func TestReplyOnlyOnce(t *testing.T) {
	p := &Pending{reply: make(chan Response, 1)}
	p.Reply(Response{OK: true})
	p.Fail(errors.New("late")) // must not block or replace
	if r := <-p.reply; !r.OK {
		t.Fatal("second reply replaced the first")
	}
}

func TestFindSocketPrefersEnv(t *testing.T) {
	t.Setenv(SocketEnv, "/tmp/explicit.sock")
	got, err := FindSocket()
	if err != nil || got != "/tmp/explicit.sock" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestFindSocketScansAndPrunes(t *testing.T) {
	dir := shortDir(t)
	srv := listenAt(t, dir)
	t.Setenv(SocketEnv, "")
	// A leftover from a crashed ct: a socket file nobody listens on.
	stale := filepath.Join(dir, "ct", "ct-1.sock")
	ln, err := net.Listen("unix", stale)
	if err != nil {
		t.Fatal(err)
	}
	ln.Close() // unix listeners remove their file on close; recreate a dead one
	if err := os.WriteFile(stale, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := FindSocket()
	if err != nil {
		t.Fatal(err)
	}
	if got != srv.Path() {
		t.Fatalf("got %q want %q", got, srv.Path())
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale socket not pruned: %v", err)
	}
}

func TestFindSocketNone(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", shortDir(t))
	t.Setenv(SocketEnv, "")
	// Only the fallback dir could hold a live socket from a real ct on this
	// machine; the test can't rule that out, so it only checks the no-socket
	// state dir doesn't error on its own.
	if _, err := FindSocket(); err != nil && !strings.Contains(err.Error(), "no running ct") &&
		!strings.Contains(err.Error(), "several ct") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLongStateDirFallsBackToShortSocket(t *testing.T) {
	long := filepath.Join(t.TempDir(), strings.Repeat("deep/", 20))
	t.Setenv("XDG_STATE_HOME", long)
	srv, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if strings.HasPrefix(srv.Path(), long) || len(srv.Path()) > maxSocketPath {
		t.Fatalf("socket at %q", srv.Path())
	}
}

func TestCloseUnblocksPending(t *testing.T) {
	srv := listenAt(t, shortDir(t))
	done := make(chan error, 1)
	go func() {
		_, err := Call(srv.Path(), Request{Op: OpList})
		done <- err
	}()
	<-srv.Requests() // take the request, never reply
	srv.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error after the server closed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("client hung after server close")
	}
}

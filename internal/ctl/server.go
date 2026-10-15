package ctl

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// replyTimeout bounds how long a connection waits for the TUI to answer. Every
// op replies quickly except spawn, which waits for `git worktree add`.
const replyTimeout = 5 * time.Minute

// Pending is one request waiting for the TUI. The TUI answers it with Reply,
// from any goroutine, exactly once; later calls are ignored.
type Pending struct {
	Req   Request
	reply chan Response
	once  sync.Once
}

// Reply answers the request. Extra calls are dropped so a handler that replies
// early on an error can't block on a second answer.
func (p *Pending) Reply(resp Response) {
	p.once.Do(func() { p.reply <- resp })
}

// Fail is Reply with an error.
func (p *Pending) Fail(err error) {
	p.Reply(Response{Error: err.Error()})
}

// Server accepts ctl connections on a Unix socket and hands each request to
// the TUI through Requests. It owns the socket file and removes it on Close.
type Server struct {
	path     string
	ln       net.Listener
	requests chan *Pending
	closed   chan struct{}
	once     sync.Once
}

// SocketDirs lists where ct instances publish their sockets, in order of
// preference: ct's state directory (honouring XDG_STATE_HOME), then a short
// per-user directory under /tmp. Unix socket paths are limited to about a
// hundred bytes, which a long state directory can exceed; the short one is
// the fallback for that case.
func SocketDirs() ([]string, error) {
	short := filepath.Join(os.TempDir(), fmt.Sprintf("ct-%d", os.Getuid()))
	if runtime.GOOS == "darwin" {
		short = fmt.Sprintf("/tmp/ct-%d", os.Getuid()) // $TMPDIR is long on macOS
	}
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return []string{filepath.Join(x, "ct"), short}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return []string{filepath.Join(home, ".local", "state", "ct"), short}, nil
}

// maxSocketPath is the longest socket path that binds on every supported
// platform (sun_path is 104 bytes on macOS, including the terminator).
const maxSocketPath = 100

// Listen starts a server on a socket named after this process, so several ct
// instances can run side by side. The returned path is also what child
// processes should get in CT_SOCKET.
func Listen() (*Server, error) {
	dirs, err := SocketDirs()
	if err != nil {
		return nil, err
	}
	name := fmt.Sprintf("ct-%d.sock", os.Getpid())
	var path string
	for _, dir := range dirs {
		if p := filepath.Join(dir, name); len(p) <= maxSocketPath {
			path = p
			break
		}
	}
	if path == "" {
		return nil, fmt.Errorf("no socket directory short enough (tried %v)", dirs)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	_ = os.Remove(path) // a stale socket from a crashed ct with the same pid
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	s := &Server{path: path, ln: ln, requests: make(chan *Pending), closed: make(chan struct{})}
	go s.accept()
	return s, nil
}

// Path is the socket's filesystem path.
func (s *Server) Path() string { return s.path }

// Requests delivers each incoming request. The TUI reads it from a Bubble Tea
// command and must Reply to every Pending it takes.
func (s *Server) Requests() <-chan *Pending { return s.requests }

// Close stops accepting and removes the socket file.
func (s *Server) Close() {
	s.once.Do(func() {
		close(s.closed)
		_ = s.ln.Close()
		_ = os.Remove(s.path)
	})
}

func (s *Server) accept() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			select {
			case <-s.closed:
				return
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		go s.serve(conn)
	}
}

// serve handles one connection: one request line in, one response line out.
func (s *Server) serve(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	var req Request
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&req); err != nil {
		writeResponse(conn, Response{Error: "bad request: " + err.Error()})
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	p := &Pending{Req: req, reply: make(chan Response, 1)}
	select {
	case s.requests <- p:
	case <-s.closed:
		writeResponse(conn, Response{Error: "ct is shutting down"})
		return
	case <-time.After(replyTimeout):
		writeResponse(conn, Response{Error: "ct did not pick up the request"})
		return
	}
	select {
	case resp := <-p.reply:
		writeResponse(conn, resp)
	case <-time.After(replyTimeout):
		writeResponse(conn, Response{Error: "timed out waiting for ct"})
	case <-s.closed:
		writeResponse(conn, Response{Error: "ct is shutting down"})
	}
}

func writeResponse(conn net.Conn, resp Response) {
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_ = json.NewEncoder(conn).Encode(resp)
}

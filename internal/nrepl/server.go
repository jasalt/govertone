package nrepl

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	musiclisp "github.com/example/letgo-sointu/internal/lisp"
	"github.com/nooga/let-go/pkg/vm"
	"github.com/zeebo/bencode"
)

const (
	defaultMaxConnections = 32
	defaultMaxSessions    = 128
	defaultMaxOutput      = 1 << 20
)

// Config controls the bounded network-facing parts of the server.
type Config struct {
	Bind           string
	Port           int
	PortFile       string
	WritePortFile  bool
	MaxConnections int
	MaxSessions    int
	MaxOutputBytes int
}

type session struct {
	id        string
	namespace string
}

// Server is a small nREPL protocol adapter. All evaluation is delegated to the
// Runtime's serialized control-side evaluator; the server never accesses audio
// engine internals.
type Server struct {
	runtime *musiclisp.Runtime
	config  Config

	mu       sync.Mutex
	listener net.Listener
	stopping bool
	sessions map[string]*session
	conns    map[net.Conn]struct{}
	sem      chan struct{}
	wg       sync.WaitGroup
}

func New(runtime *musiclisp.Runtime, config Config) *Server {
	if config.Bind == "" {
		config.Bind = "127.0.0.1"
	}
	if config.PortFile == "" {
		config.PortFile = ".nrepl-port"
	}
	if config.MaxConnections <= 0 {
		config.MaxConnections = defaultMaxConnections
	}
	if config.MaxSessions <= 0 {
		config.MaxSessions = defaultMaxSessions
	}
	if config.MaxOutputBytes <= 0 {
		config.MaxOutputBytes = defaultMaxOutput
	}
	return &Server{
		runtime:  runtime,
		config:   config,
		sessions: make(map[string]*session),
		conns:    make(map[net.Conn]struct{}),
		sem:      make(chan struct{}, config.MaxConnections),
	}
}

func (s *Server) Start() error {
	if s.runtime == nil {
		return errors.New("nREPL runtime is nil")
	}
	if s.config.Port < 0 || s.config.Port > 65535 {
		return fmt.Errorf("invalid nREPL port %d", s.config.Port)
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(s.config.Bind, strconv.Itoa(s.config.Port)))
	if err != nil {
		return fmt.Errorf("nrepl-bind-failed: %w", err)
	}
	if s.config.WritePortFile {
		port := listener.Addr().(*net.TCPAddr).Port
		if err = os.WriteFile(s.config.PortFile, []byte(strconv.Itoa(port)), 0o644); err != nil {
			_ = listener.Close()
			return fmt.Errorf("write nREPL port file: %w", err)
		}
	}

	s.mu.Lock()
	s.listener = listener
	s.stopping = false
	s.mu.Unlock()
	s.wg.Add(1)
	go s.accept()
	return nil
}

func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

func (s *Server) Port() int {
	addr := s.Addr()
	if addr == nil {
		return 0
	}
	return addr.(*net.TCPAddr).Port
}

func (s *Server) Stop() error {
	s.mu.Lock()
	if s.listener == nil || s.stopping {
		s.mu.Unlock()
		return nil
	}
	s.stopping = true
	listener := s.listener
	for conn := range s.conns {
		_ = conn.Close()
	}
	s.mu.Unlock()
	_ = listener.Close()
	s.wg.Wait()
	if s.config.WritePortFile {
		_ = os.Remove(s.config.PortFile)
	}
	return nil
}

func (s *Server) accept() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			s.mu.Lock()
			stopping := s.stopping
			s.mu.Unlock()
			if stopping {
				return
			}
			continue
		}
		select {
		case s.sem <- struct{}{}:
			s.mu.Lock()
			s.conns[conn] = struct{}{}
			s.mu.Unlock()
			s.wg.Add(1)
			go s.handleConn(conn)
		default:
			_ = conn.Close()
		}
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer func() {
		_ = conn.Close()
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
		<-s.sem
		s.wg.Done()
	}()
	decoder := bencode.NewDecoder(conn)
	for {
		var msg map[string]any
		if err := decoder.Decode(&msg); err != nil {
			return
		}
		s.handle(conn, msg)
	}
}

func (s *Server) handle(conn net.Conn, msg map[string]any) {
	op, id, sessionID := msgString(msg, "op"), msgString(msg, "id"), msgString(msg, "session")
	switch op {
	case "clone":
		s.clone(conn, id)
	case "close":
		s.close(conn, id, sessionID)
	case "describe":
		s.respond(conn, map[string]any{
			"id": id, "session": sessionID, "status": []string{"done"},
			"ops": map[string]any{
				"clone": map[string]any{}, "close": map[string]any{}, "describe": map[string]any{},
				"eval": map[string]any{}, "interrupt": map[string]any{}, "load-file": map[string]any{},
				"stdin": map[string]any{}, "ls-sessions": map[string]any{},
			},
			"versions": map[string]any{"let-go": map[string]any{"major": "1", "minor": "11"}, "nrepl": map[string]any{"major": "1", "minor": "0"}},
		})
	case "eval":
		s.eval(conn, id, sessionID, msgString(msg, "code"))
	case "load-file":
		s.eval(conn, id, sessionID, msgString(msg, "file"))
	case "interrupt":
		// let-go v1.11.1 has no evaluator cancellation primitive. A protocol
		// interrupt is therefore safe and cooperative: acknowledged music work
		// is retained and audio continues.
		s.respond(conn, map[string]any{"id": id, "session": sessionID, "status": []string{"done", "session-idle"}})
	case "stdin":
		// Runtime forms do not currently consume nREPL stdin. Accepting the op
		// keeps clients from hanging while making no process-global input swap.
		s.respond(conn, map[string]any{"id": id, "session": sessionID, "status": []string{"done"}})
	case "ls-sessions":
		s.mu.Lock()
		ids := make([]string, 0, len(s.sessions))
		for sessionID := range s.sessions {
			ids = append(ids, sessionID)
		}
		s.mu.Unlock()
		s.respond(conn, map[string]any{"id": id, "session": sessionID, "sessions": ids, "status": []string{"done"}})
	default:
		s.respond(conn, map[string]any{"id": id, "session": sessionID, "status": []string{"done", "error", "unknown-op"}})
	}
}

func (s *Server) clone(conn net.Conn, id string) {
	s.mu.Lock()
	if len(s.sessions) >= s.config.MaxSessions {
		s.mu.Unlock()
		s.respond(conn, map[string]any{"id": id, "status": []string{"done", "error", "session-limit-exceeded"}})
		return
	}
	sessionID := randomID()
	s.sessions[sessionID] = &session{id: sessionID, namespace: "music.core"}
	s.mu.Unlock()
	s.respond(conn, map[string]any{"id": id, "new-session": sessionID, "status": []string{"done"}})
}

func (s *Server) close(conn net.Conn, id, sessionID string) {
	s.mu.Lock()
	_, found := s.sessions[sessionID]
	delete(s.sessions, sessionID)
	s.mu.Unlock()
	status := []string{"done", "session-closed"}
	if !found {
		status = []string{"done", "error", "nrepl-session-not-found"}
	}
	s.respond(conn, map[string]any{"id": id, "session": sessionID, "status": status})
}

func (s *Server) eval(conn net.Conn, id, sessionID, code string) {
	namespace := "music.core"
	if sessionID != "" {
		s.mu.Lock()
		sess := s.sessions[sessionID]
		if sess != nil {
			namespace = sess.namespace
		}
		s.mu.Unlock()
		if sess == nil {
			s.respond(conn, map[string]any{"id": id, "session": sessionID, "status": []string{"done", "error", "nrepl-session-not-found"}})
			return
		}
	}

	out, errOut := newBoundedBuffer(s.config.MaxOutputBytes), newBoundedBuffer(s.config.MaxOutputBytes)
	value, resultingNS, err := s.runtime.EvalInNamespace(code, namespace, out, errOut)
	if sessionID != "" {
		s.mu.Lock()
		if sess := s.sessions[sessionID]; sess != nil {
			sess.namespace = resultingNS
		}
		s.mu.Unlock()
	}
	if out.Len() > 0 {
		s.respond(conn, map[string]any{"id": id, "session": sessionID, "out": out.String()})
	}
	if errOut.Len() > 0 {
		s.respond(conn, map[string]any{"id": id, "session": sessionID, "err": errOut.String()})
	}
	if out.Overflowed() || errOut.Overflowed() {
		s.respond(conn, map[string]any{"id": id, "session": sessionID, "err": "nREPL evaluation output exceeded configured limit\n", "status": []string{"error", "nrepl-output-overflow"}})
	}
	if err != nil {
		s.respond(conn, map[string]any{"id": id, "session": sessionID, "err": vm.FormatError(err) + "\n", "ex": "let-go.lang.Error", "root-ex": "let-go.lang.Error"})
	} else {
		valueString := "nil"
		if value != nil {
			valueString = value.String()
		}
		s.respond(conn, map[string]any{"id": id, "session": sessionID, "value": valueString, "ns": resultingNS})
	}
	s.respond(conn, map[string]any{"id": id, "session": sessionID, "status": []string{"done"}})
}

func (s *Server) respond(conn net.Conn, msg map[string]any) {
	encoded, err := bencode.EncodeBytes(msg)
	if err != nil {
		return
	}
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, _ = conn.Write(encoded)
}

func msgString(msg map[string]any, key string) string {
	value := msg[key]
	switch value := value.(type) {
	case string:
		return value
	case int64:
		return strconv.FormatInt(value, 10)
	default:
		return ""
	}
}

func randomID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return fmt.Sprintf("session-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(value[:])
}

type boundedBuffer struct {
	buffer     bytes.Buffer
	remaining  int
	overflowed bool
}

func newBoundedBuffer(limit int) *boundedBuffer { return &boundedBuffer{remaining: limit} }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	original := len(p)
	if len(p) > b.remaining {
		p = p[:b.remaining]
		b.overflowed = true
	}
	if len(p) > 0 {
		_, _ = b.buffer.Write(p)
		b.remaining -= len(p)
	}
	return original, nil
}
func (b *boundedBuffer) Len() int         { return b.buffer.Len() }
func (b *boundedBuffer) String() string   { return b.buffer.String() }
func (b *boundedBuffer) Overflowed() bool { return b.overflowed }

var _ io.Writer = (*boundedBuffer)(nil)

package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"hue/internal/config"
	"hue/internal/hue"
)

// Request is one command sent to the long-lived hue serve process.
type Request struct {
	Op         string   `json:"op"`
	Kind       string   `json:"kind,omitempty"`
	ID         string   `json:"id,omitempty"`
	On         *bool    `json:"on,omitempty"`
	Brightness *float64 `json:"brightness,omitempty"`
	Hue        *float64 `json:"hue,omitempty"`
	Saturation *float64 `json:"saturation,omitempty"`
	CT         *int     `json:"ct,omitempty"`
}

// SocketPath is the per-user socket the panel and the CLI share.
func SocketPath() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "hue.sock")
}

// Forward sends a command to a running server. The bool is false when no
// server is listening, so the caller can do the work itself.
func Forward(req Request) (json.RawMessage, bool) {
	conn, err := net.DialTimeout("unix", SocketPath(), 80*time.Millisecond)
	if err != nil {
		return nil, false
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(8 * time.Second))
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, false
	}
	var raw json.RawMessage
	if err := json.NewDecoder(conn).Decode(&raw); err != nil {
		return nil, false
	}
	return raw, true
}

// Serve listens until the process is signaled. A second serve exits
// immediately when one is already running.
func Serve() error {
	if ping() {
		return nil
	}
	path := SocketPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		if ping() {
			return nil
		}
		return err
	}
	_ = os.Chmod(path, 0o600)
	defer ln.Close()
	defer os.Remove(path)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		ln.Close()
	}()

	var session bridgeSession
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go session.handle(conn)
	}
}

func ping() bool {
	raw, ok := Forward(Request{Op: "ping"})
	if !ok {
		return false
	}
	var probe struct {
		OK bool `json:"ok"`
	}
	return json.Unmarshal(raw, &probe) == nil && probe.OK
}

// bridgeSession serializes bridge calls on one keep-alive connection.
// The bridge handles a single TLS session much faster than overlapping ones.
type bridgeSession struct {
	mu     sync.Mutex
	client *hue.Client
	ip     string
	user   string
}

func (s *bridgeSession) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	dec := json.NewDecoder(bufio.NewReader(conn))
	var req Request
	if err := dec.Decode(&req); err != nil {
		writeConn(conn, Result{Error: err.Error()})
		return
	}
	writeConn(conn, s.dispatch(req))
}

func (s *bridgeSession) dispatch(req Request) any {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := contextFor(req.Op)
	defer cancel()
	switch req.Op {
	case "ping":
		return Result{OK: true}
	case "snapshot":
		return s.snapshot(ctx)
	case "set":
		result, err := s.set(ctx, req)
		if err != nil {
			return Result{Error: err.Error()}
		}
		return result
	case "scene":
		result, err := s.activate(ctx, req)
		if err != nil {
			return Result{Error: err.Error()}
		}
		return result
	case "forget":
		s.client = nil
		return Forget()
	default:
		return Result{Error: "unknown command"}
	}
}

func (s *bridgeSession) snapshot(ctx context.Context) any {
	cfg, err := config.Load()
	if err != nil {
		return Result{Error: err.Error()}
	}
	if cfg == nil {
		snap, err := Snapshot(ctx)
		if err != nil {
			return Result{Error: err.Error()}
		}
		return snap
	}
	client, err := s.warm()
	if err != nil {
		return Result{Error: err.Error()}
	}
	snap, err := snapshotWith(ctx, client, cfg)
	if err != nil {
		if unreachable(err) {
			s.client = nil
		}
		return Result{Error: err.Error()}
	}
	return snap
}

func (s *bridgeSession) set(ctx context.Context, req Request) (Result, error) {
	if req.Kind != "light" && req.Kind != "grouped_light" {
		return Result{}, errors.New("kind must be light or grouped_light")
	}
	if !validID(req.ID) {
		return Result{}, errors.New("missing resource id")
	}
	client, err := s.warm()
	if err != nil {
		return Result{}, err
	}
	patch := Patch{On: req.On, Brightness: req.Brightness, Hue: req.Hue, Saturation: req.Saturation, CT: req.CT}
	if err := apply(ctx, client, req.Kind, req.ID, patch); err != nil {
		if unreachable(err) {
			s.client = nil
		}
		return Result{}, cleanErr(err)
	}
	return Result{OK: true}, nil
}

func (s *bridgeSession) activate(ctx context.Context, req Request) (Result, error) {
	kind := req.Kind
	if kind == "" {
		kind = "scene"
	}
	if kind != "scene" && kind != "smart_scene" {
		return Result{}, errors.New("kind must be scene or smart_scene")
	}
	if !validID(req.ID) {
		return Result{}, errors.New("missing scene id")
	}
	client, err := s.warm()
	if err != nil {
		return Result{}, err
	}
	action := "active"
	if kind == "smart_scene" {
		action = "activate"
	}
	body := map[string]any{"recall": map[string]any{"action": action}}
	if err := client.Put(ctx, kind, req.ID, body); err != nil {
		if unreachable(err) {
			s.client = nil
		}
		return Result{}, cleanErr(err)
	}
	return Result{OK: true}, nil
}

func (s *bridgeSession) warm() (*hue.Client, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, errors.New("not paired with a bridge")
	}
	if s.client == nil || s.ip != cfg.IP || s.user != cfg.Username {
		s.client = hue.NewWarmClient(cfg.IP, cfg.Username)
		s.ip = cfg.IP
		s.user = cfg.Username
	}
	return s.client, nil
}

func contextFor(op string) (ctx context.Context, cancel func()) {
	limit := 8 * time.Second
	if op == "snapshot" {
		limit = 12 * time.Second
	}
	return context.WithTimeout(context.Background(), limit)
}

func writeConn(conn net.Conn, v any) {
	enc := json.NewEncoder(conn)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

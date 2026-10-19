package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// StatusPath and StopPath are the HTTP paths the control socket serves.
const (
	StatusPath = "/status"
	StopPath   = "/stop"
)

const (
	requestTimeout  = 5 * time.Second
	shutdownTimeout = 2 * time.Second
)

// Status is what a running ZAB reports about itself.
type Status struct {
	// Version is the ZAB build.
	Version string `json:"version"`

	// StartedAt is when the running ZAB started.
	StartedAt time.Time `json:"startedAt"`

	// ConfigFile is the configuration file the running ZAB loaded.
	ConfigFile string `json:"configFile"`

	// Controllers lists the OpenZiti controller addresses the running ZAB knows.
	Controllers []string `json:"controllers"`

	// ActiveController is the OpenZiti controller the running ZAB's own identity is
	// currently authenticated against.
	ActiveController string `json:"activeController"`

	// TeamDomain is the Cloudflare Access team the running ZAB accepts requests from.
	TeamDomain string `json:"teamDomain"`

	// PoolSize is the number of requester OpenZiti SDK contexts held.
	PoolSize int `json:"poolSize"`
}

// Source is what a running ZAB exposes over the control socket.
type Source interface {
	// Status reports the running ZAB's state.
	Status() Status

	// Stop asks the running ZAB to shut down.
	Stop()
}

// Serve listens on the unix socket at path and serves src until ctx is done. It blocks and
// returns nil after a clean shutdown.
func Serve(ctx context.Context, path string, src Source) error {
	if err := removeStale(path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("control socket directory could not be created: %w", err)
	}

	listener, err := net.Listen("unix", path)
	if err != nil {
		return fmt.Errorf("control socket could not be opened: %w", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o600); err != nil {
			_ = listener.Close()
			return fmt.Errorf("control socket permissions could not be set: %w", err)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET "+StatusPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(src.Status())
	})
	mux.HandleFunc("POST "+StopPath, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		go src.Stop()
	})

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: requestTimeout,
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	err = server.Serve(listener)
	<-done
	_ = os.Remove(path)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func removeStale(path string) error {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err == nil {
		_ = conn.Close()
		return fmt.Errorf("control socket %s is in use by a running zab", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("stale control socket could not be removed: %w", err)
	}
	return nil
}

// Client talks to a running ZAB over its control socket.
type Client struct {
	http *http.Client
}

// NewClient builds a Client for the control socket at path.
func NewClient(path string) *Client {
	return &Client{
		http: &http.Client{
			Timeout: requestTimeout,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", path)
				},
			},
		},
	}
}

// Status fetches the running ZAB's Status.
func (c *Client) Status(ctx context.Context) (*Status, error) {
	resp, err := c.do(ctx, http.MethodGet, StatusPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	status := &Status{}
	if err := json.NewDecoder(resp.Body).Decode(status); err != nil {
		return nil, fmt.Errorf("status response could not be decoded: %w", err)
	}
	return status, nil
}

// Stop asks the running ZAB to shut down.
func (c *Client) Stop(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodPost, StopPath)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

func (c *Client) do(ctx context.Context, method, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, "http://zab"+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("zab is not reachable on its control socket: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("zab answered %s: %s", resp.Status, string(body))
	}
	return resp, nil
}

package control

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeSource struct {
	status  Status
	stopped chan struct{}
}

func (f *fakeSource) Status() Status {
	return f.status
}

func (f *fakeSource) Stop() {
	close(f.stopped)
}

func socketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "zab")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "zab.sock")
}

func waitForSocket(t *testing.T, client *Client) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		_, err := client.Status(ctx)
		cancel()
		if err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("control socket did not come up")
}

func TestSocket(t *testing.T) {
	t.Run("status and stop travel over the unix socket", func(t *testing.T) {
		path := socketPath(t)
		src := &fakeSource{
			status:  Status{Version: "test", Controllers: []string{"https://a/edge/client/v1"}, PoolSize: 3},
			stopped: make(chan struct{}),
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		served := make(chan error, 1)
		go func() { served <- Serve(ctx, path, src) }()
		client := NewClient(path)
		waitForSocket(t, client)

		status, err := client.Status(context.Background())
		require.NoError(t, err)
		require.NoError(t, client.Stop(context.Background()))

		require.Equal(t, src.status, *status)
		select {
		case <-src.stopped:
		case <-time.After(5 * time.Second):
			t.Fatal("stop did not reach the source")
		}
		cancel()
		require.NoError(t, <-served)
		_, statErr := os.Stat(path)
		require.ErrorIs(t, statErr, os.ErrNotExist, "the socket file must be removed on shutdown")
	})

	t.Run("a stale socket file is replaced", func(t *testing.T) {
		path := socketPath(t)
		require.NoError(t, os.WriteFile(path, nil, 0o600))
		src := &fakeSource{stopped: make(chan struct{})}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		served := make(chan error, 1)
		go func() { served <- Serve(ctx, path, src) }()
		client := NewClient(path)

		waitForSocket(t, client)

		cancel()
		require.NoError(t, <-served)
	})

	t.Run("a live socket is not taken over", func(t *testing.T) {
		path := socketPath(t)
		src := &fakeSource{stopped: make(chan struct{})}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		served := make(chan error, 1)
		go func() { served <- Serve(ctx, path, src) }()
		waitForSocket(t, NewClient(path))

		err := Serve(context.Background(), path, src)

		require.Error(t, err)
		cancel()
		require.NoError(t, <-served)
	})
}

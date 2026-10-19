package metrics

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Path is the HTTP path Serve exposes the Prometheus metrics on.
const Path = "/metrics"

const shutdownTimeout = 5 * time.Second

// Serve exposes prometheus.DefaultGatherer at Path on address until ctx is done. It blocks
// and returns nil after a clean shutdown.
func Serve(ctx context.Context, address string) error {
	mux := http.NewServeMux()
	mux.Handle(Path, promhttp.Handler())

	server := &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: shutdownTimeout,
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	err := server.ListenAndServe()
	<-done
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

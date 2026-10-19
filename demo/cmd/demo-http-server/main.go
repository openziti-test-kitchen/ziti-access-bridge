package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/openziti-test-kitchen/ziti-access-bridge/cfaccess"
)

type echo struct {
	Method     string              `json:"method"`
	Path       string              `json:"path"`
	Query      string              `json:"query,omitempty"`
	Host       string              `json:"host"`
	RemoteAddr string              `json:"remoteAddr"`
	TLS        bool                `json:"tls"`
	Headers    map[string][]string `json:"headers"`
	Claims     map[string]any      `json:"cloudflareAccessClaims,omitempty"`
	ReceivedAt time.Time           `json:"receivedAt"`
}

func main() {
	var (
		iface      string
		port       int
		teamDomain string
		audTags    []string
		tlsCert    string
		tlsKey     string
	)

	cmd := &cobra.Command{
		Use:           "demo-http-server",
		Short:         "Echo HTTP server for the far end of a ZAB demo",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var verifier *cfaccess.Verifier
			if teamDomain != "" || len(audTags) > 0 {
				v, err := cfaccess.NewVerifier(cmd.Context(), teamDomain, audTags)
				if err != nil {
					return err
				}
				verifier = v
			}
			if (tlsCert == "") != (tlsKey == "") {
				return errors.New("--tls-cert and --tls-key go together")
			}
			return serve(cmd.Context(), net.JoinHostPort(iface, strconv.Itoa(port)), verifier, tlsCert, tlsKey)
		},
	}

	f := cmd.Flags()
	f.StringVar(&iface, "interface", "127.0.0.1", "Address to listen on")
	f.IntVar(&port, "port", 8080, "Port to listen on")
	f.StringVar(&teamDomain, "cloudflare-team-domain", "", "Cloudflare Access team domain. With --cloudflare-aud, every request must carry a valid Cloudflare Access JWT")
	f.StringArrayVar(&audTags, "cloudflare-aud", nil, "Cloudflare Access application AUD tag to accept. Repeatable")
	f.StringVar(&tlsCert, "tls-cert", "", "PEM certificate to serve HTTPS with")
	f.StringVar(&tlsKey, "tls-key", "", "PEM key for --tls-cert")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := cmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "demo-http-server:", err)
		os.Exit(1)
	}
}

func serve(ctx context.Context, address string, verifier *cfaccess.Verifier, tlsCert, tlsKey string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.Handle("/echo", echoHandler(verifier))
	mux.Handle("/", pageHandler(verifier))

	server := &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	mode := "echo only"
	if verifier != nil {
		mode = "verifying cloudflare access jwts for " + verifier.Issuer()
	}
	scheme := "http"
	var err error
	if tlsCert != "" {
		scheme = "https"
		log.Printf("listening on %s://%s, %s", scheme, address, mode)
		err = server.ListenAndServeTLS(tlsCert, tlsKey)
	} else {
		log.Printf("listening on %s://%s, %s", scheme, address, mode)
		err = server.ListenAndServe()
	}
	<-done
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func echoHandler(verifier *cfaccess.Verifier) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e := echo{
			Method:     r.Method,
			Path:       r.URL.Path,
			Query:      r.URL.RawQuery,
			Host:       r.Host,
			RemoteAddr: r.RemoteAddr,
			TLS:        r.TLS != nil,
			Headers:    r.Header,
			ReceivedAt: time.Now(),
		}

		if verifier != nil {
			raw := r.Header.Get(cfaccess.Header)
			if raw == "" {
				log.Printf("%s %s from %s: no cloudflare access jwt", r.Method, r.URL.Path, r.RemoteAddr)
				http.Error(w, "missing cloudflare access jwt", http.StatusUnauthorized)
				return
			}
			claims, err := verifier.Verify(r.Context(), raw)
			if err != nil {
				log.Printf("%s %s from %s: %v", r.Method, r.URL.Path, r.RemoteAddr, err)
				http.Error(w, "invalid cloudflare access jwt", http.StatusUnauthorized)
				return
			}
			e.Claims = claims.All
		}

		log.Printf("%s %s host=%s from %s", r.Method, r.URL.Path, r.Host, r.RemoteAddr)
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(e)
	})
}

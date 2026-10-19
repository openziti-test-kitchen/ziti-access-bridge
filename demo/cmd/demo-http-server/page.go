package main

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/openziti-test-kitchen/ziti-access-bridge/cfaccess"
)

//go:embed page.html
var pageHTML string

var page = template.Must(template.New("page").Parse(pageHTML))

// pageData is what page.html renders.
type pageData struct {
	Error string
	Found bool

	Email    string
	Host     string
	Now      string
	NowUTC   string
	Verified bool
	Issuer   string

	Header        string
	Payload       string
	Signature     string
	HeaderJSON    string
	PayloadJSON   string
	SignatureNote string
}

// pageHandler renders the dark themed landing page: an error when the request carries no
// valid Cloudflare Access JWT, otherwise a greeting, the verification status, and the JWT
// encoded beside its decoded header and payload.
func pageHandler(verifier *cfaccess.Verifier) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := pageData{Host: r.Host}
		status := http.StatusOK

		raw := r.Header.Get(cfaccess.Header)
		switch {
		case raw == "":
			data.Error = "This request carries no Cloudflare Access JWT. It did not come through Cloudflare Access."
			status = http.StatusUnauthorized
		case verifier != nil:
			claims, err := verifier.Verify(r.Context(), raw)
			if err != nil {
				data.Found = true
				data.Error = "The Cloudflare Access JWT on this request did not verify: " + err.Error()
				status = http.StatusUnauthorized
				break
			}
			data.Verified = true
			data.Issuer = verifier.Issuer()
			data.Email = claims.String("email")
			if data.Email == "" {
				data.Email = claims.Subject
			}
		default:
			data.Email = unverifiedEmail(raw)
		}

		if data.Error == "" {
			now := time.Now()
			data.Now = now.UTC().Format(time.RFC3339)
			data.NowUTC = now.UTC().Format("Mon, 02 Jan 2006 15:04:05 UTC")
			if err := fillJWT(&data, raw); err != nil {
				data.Error = "The Cloudflare Access JWT could not be decoded: " + err.Error()
				data.Found = true
				status = http.StatusUnauthorized
			}
		}

		log.Printf("GET %s host=%s from %s: %d", r.URL.Path, r.Host, r.RemoteAddr, status)

		var buf bytes.Buffer
		if err := page.Execute(&buf, data); err != nil {
			http.Error(w, "page could not be rendered", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		_, _ = w.Write(buf.Bytes())
	})
}

// fillJWT splits raw into its three segments and decodes the header and payload for
// display. The signature is shown as received.
func fillJWT(data *pageData, raw string) error {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return errors.New("expected three dot separated segments")
	}
	data.Header, data.Payload, data.Signature = parts[0], parts[1], parts[2]

	header, err := decodeSegment(parts[0])
	if err != nil {
		return err
	}
	payload, err := decodeSegment(parts[1])
	if err != nil {
		return err
	}
	data.HeaderJSON = header
	data.PayloadJSON = payload
	if data.Verified {
		data.SignatureNote = "Verified against the signing key the Cloudflare Access team publishes at " + data.Issuer + "/cdn-cgi/access/certs, selected by the header's kid."
	} else {
		data.SignatureNote = "Not checked. Start this server with --cloudflare-team-domain and --cloudflare-aud to verify it."
	}
	return nil
}

func decodeSegment(segment string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		return "", err
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, decoded, "", "  "); err != nil {
		return "", err
	}
	return pretty.String(), nil
}

// unverifiedEmail reads the email claim out of an unverified JWT, for the greeting when the
// server runs without Cloudflare flags.
func unverifiedEmail(raw string) string {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return "there"
	}
	decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "there"
	}
	var claims struct {
		Email string `json:"email"`
		Sub   string `json:"sub"`
	}
	if json.Unmarshal(decoded, &claims) != nil {
		return "there"
	}
	if claims.Email != "" {
		return claims.Email
	}
	if claims.Sub != "" {
		return claims.Sub
	}
	return "there"
}

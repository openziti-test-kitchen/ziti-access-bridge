package main

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/openziti-test-kitchen/ziti-access-bridge/cfaccess"
)

func unsignedJWT(t *testing.T, header, payload string) string {
	t.Helper()
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(header)) + "." + enc([]byte(payload)) + ".signature"
}

func TestPage(t *testing.T) {
	t.Run("no jwt is a 401 error page", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		pageHandler(nil).ServeHTTP(rec, req)

		require.Equal(t, http.StatusUnauthorized, rec.Code)
		require.Contains(t, rec.Body.String(), "Not authenticated")
		require.Contains(t, rec.Body.String(), "not found")
	})

	t.Run("unverified jwt renders the greeting and both columns", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set(cfaccess.Header, unsignedJWT(t, `{"alg":"RS256","kid":"k1"}`, `{"email":"jdoe@example.com","sub":"abc"}`))
		rec := httptest.NewRecorder()

		pageHandler(nil).ServeHTTP(rec, req)

		body := rec.Body.String()
		require.Equal(t, http.StatusOK, rec.Code)
		require.Contains(t, body, "Hi jdoe@example.com")
		require.Contains(t, body, `&#34;kid&#34;: &#34;k1&#34;`, "decoded header is rendered html escaped")
		require.Contains(t, body, `&#34;email&#34;: &#34;jdoe@example.com&#34;`, "decoded payload is rendered html escaped")
		require.Contains(t, body, "not verified")
		require.Contains(t, body, `<time id="now"`)
	})

	t.Run("malformed jwt is a 401 error page", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set(cfaccess.Header, "not.a.jwt.at.all")
		rec := httptest.NewRecorder()

		pageHandler(nil).ServeHTTP(rec, req)

		require.Equal(t, http.StatusUnauthorized, rec.Code)
		require.Contains(t, rec.Body.String(), "could not be decoded")
	})

	t.Run("claims are html escaped", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set(cfaccess.Header, unsignedJWT(t, `{"alg":"RS256"}`, `{"email":"<script>x</script>"}`))
		rec := httptest.NewRecorder()

		pageHandler(nil).ServeHTTP(rec, req)

		require.False(t, strings.Contains(rec.Body.String(), "<script>x</script>"), "claim values must be escaped")
	})
}

package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func writeConfig(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "config.yml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestLoad(t *testing.T) {
	t.Run("relative paths resolve against the config directory", func(t *testing.T) {
		dir := t.TempDir()
		path := writeConfig(t, dir, `
identity:
  key: ./zab.key
  cert: file:certs/zab.cert
  ca: zab.ca
endpointFile: state/endpoints.yml
cloudflare:
  tunnelTokenFile: tunnel.token
`)

		cfg, err := Load(path)

		require.NoError(t, err)
		require.Equal(t, filepath.Join(dir, "zab.key"), cfg.Identity.Key)
		require.Equal(t, "file:"+filepath.Join(dir, "certs", "zab.cert"), cfg.Identity.Cert)
		require.Equal(t, filepath.Join(dir, "zab.ca"), cfg.Identity.CA)
		require.Equal(t, filepath.Join(dir, "state", "endpoints.yml"), cfg.EndpointFile)
		require.Equal(t, filepath.Join(dir, "tunnel.token"), cfg.Cloudflare.TunnelTokenFile)
		require.Equal(t, dir, cfg.Dir())
	})

	t.Run("absolute, pem, and engine identity addresses are left alone", func(t *testing.T) {
		dir := t.TempDir()
		abs := filepath.Join(t.TempDir(), "elsewhere.key")
		path := writeConfig(t, dir, `
identity:
  key: "`+filepath.ToSlash(abs)+`"
  cert: "pem:-----BEGIN CERTIFICATE-----"
  ca: "pkcs11:/usr/lib/softhsm.so?slot=0&id=1"
`)

		cfg, err := Load(path)

		require.NoError(t, err)
		require.Equal(t, filepath.ToSlash(abs), cfg.Identity.Key)
		require.Equal(t, "pem:-----BEGIN CERTIFICATE-----", cfg.Identity.Cert)
		require.Equal(t, "pkcs11:/usr/lib/softhsm.so?slot=0&id=1", cfg.Identity.CA)
	})

	t.Run("defaults apply", func(t *testing.T) {
		dir := t.TempDir()
		path := writeConfig(t, dir, "identity: {}\n")

		cfg, err := Load(path)

		require.NoError(t, err)
		require.Equal(t, filepath.Join(dir, DefaultEndpointFile), cfg.EndpointFile)
		require.Equal(t, filepath.Join(dir, DefaultSocketFile), cfg.SocketFile)
		require.Equal(t, "info", cfg.LogLevel)
	})

	t.Run("durations parse", func(t *testing.T) {
		dir := t.TempDir()
		path := writeConfig(t, dir, `
pool:
  idleWindow: 45m
  grace: 90s
dialTimeout: 3s
`)

		cfg, err := Load(path)

		require.NoError(t, err)
		require.Equal(t, 45*time.Minute, cfg.Pool.IdleWindow)
		require.Equal(t, 90*time.Second, cfg.Pool.Grace)
		require.Equal(t, 3*time.Second, cfg.DialTimeout)
	})

	t.Run("missing file is an error", func(t *testing.T) {
		_, err := Load(filepath.Join(t.TempDir(), "nope.yml"))

		require.Error(t, err)
	})
}

func TestIdentityFilePath(t *testing.T) {
	t.Run("plain and file addresses name files", func(t *testing.T) {
		plain, err := IdentityFilePath("/etc/zab/zab.key")
		require.NoError(t, err)
		prefixed, err := IdentityFilePath("file:/etc/zab/zab.key")
		require.NoError(t, err)

		require.Equal(t, "/etc/zab/zab.key", plain)
		require.Equal(t, "/etc/zab/zab.key", prefixed)
	})

	t.Run("pem and engine addresses are not files", func(t *testing.T) {
		_, pemErr := IdentityFilePath("pem:-----BEGIN")
		_, engineErr := IdentityFilePath("pkcs11:/lib.so?slot=0")
		_, emptyErr := IdentityFilePath("")

		require.Error(t, pemErr)
		require.Error(t, engineErr)
		require.Error(t, emptyErr)
	})
}

func TestFile_Validate(t *testing.T) {
	t.Run("identity without key and cert fails", func(t *testing.T) {
		cfg := &File{}
		cfg.Cloudflare.TeamDomain = "team.cloudflareaccess.com"
		cfg.Cloudflare.AUDTags = []string{"aud"}

		require.Error(t, cfg.Validate())
	})

	t.Run("complete config passes", func(t *testing.T) {
		cfg := &File{}
		cfg.Identity.Key = "zab.key"
		cfg.Identity.Cert = "zab.cert"
		cfg.Cloudflare.TeamDomain = "team.cloudflareaccess.com"
		cfg.Cloudflare.AUDTags = []string{"aud"}

		require.NoError(t, cfg.Validate())
	})
}

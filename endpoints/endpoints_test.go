package endpoints

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}

func TestFile_SetControllers(t *testing.T) {
	t.Run("sorts and deduplicates", func(t *testing.T) {
		f := &File{}

		changed := f.SetControllers([]*url.URL{
			mustURL(t, "https://b:1280/edge/client/v1"),
			mustURL(t, "https://a:1280/edge/client/v1"),
			mustURL(t, "https://b:1280/edge/client/v1"),
			nil,
		})

		require.True(t, changed)
		require.Equal(t, []string{"https://a:1280/edge/client/v1", "https://b:1280/edge/client/v1"}, f.Controllers)
	})

	t.Run("same set in another order is not a change", func(t *testing.T) {
		f := &File{Controllers: []string{"https://a/edge/client/v1", "https://b/edge/client/v1"}}

		changed := f.SetControllers([]*url.URL{
			mustURL(t, "https://b/edge/client/v1"),
			mustURL(t, "https://a/edge/client/v1"),
		})

		require.False(t, changed)
	})

	t.Run("a removed controller is a change", func(t *testing.T) {
		f := &File{Controllers: []string{"https://a/edge/client/v1", "https://b/edge/client/v1"}}

		changed := f.SetControllers([]*url.URL{mustURL(t, "https://a/edge/client/v1")})

		require.True(t, changed)
		require.Equal(t, []string{"https://a/edge/client/v1"}, f.Controllers)
	})
}

func TestSaveLoad(t *testing.T) {
	t.Run("round trip", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state", "endpoints.yml")
		saved := &File{Controllers: []string{"https://b/edge/client/v1", "https://a/edge/client/v1"}}

		require.NoError(t, Save(path, saved))
		loaded, err := Load(path)

		require.NoError(t, err)
		require.Equal(t, []string{"https://a/edge/client/v1", "https://b/edge/client/v1"}, loaded.Controllers)
	})

	t.Run("missing file loads empty", func(t *testing.T) {
		loaded, err := Load(filepath.Join(t.TempDir(), "missing.yml"))

		require.NoError(t, err)
		require.Empty(t, loaded.Controllers)
	})

	t.Run("save leaves no temp file behind", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "endpoints.yml")

		require.NoError(t, Save(path, &File{Controllers: []string{"https://a/edge/client/v1"}}))

		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		require.Len(t, entries, 1)
		require.Equal(t, "endpoints.yml", entries[0].Name())
	})
}

func TestTracker_Update(t *testing.T) {
	t.Run("writes when the set changes and leaves the file alone otherwise", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "endpoints.yml")
		tracker := NewTracker(path)
		first := []*url.URL{mustURL(t, "https://a/edge/client/v1")}

		require.NoError(t, tracker.Update(first))
		before, err := os.Stat(path)
		require.NoError(t, err)
		require.NoError(t, tracker.Update(first))
		after, err := os.Stat(path)
		require.NoError(t, err)
		require.NoError(t, tracker.Update(append(first, mustURL(t, "https://b/edge/client/v1"))))

		require.Equal(t, before.ModTime(), after.ModTime(), "an unchanged set must not rewrite the file")
		loaded, err := Load(path)
		require.NoError(t, err)
		require.Equal(t, []string{"https://a/edge/client/v1", "https://b/edge/client/v1"}, loaded.Controllers)
	})
}

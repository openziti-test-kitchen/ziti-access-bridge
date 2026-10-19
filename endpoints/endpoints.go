package endpoints

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/michaelquigley/pfxlog"
	"github.com/openziti/sdk-golang/v2/ziti"
	"gopkg.in/yaml.v3"
)

// File is the endpoint file.
type File struct {
	// Controllers lists the OpenZiti Edge Client API URLs, each of the form
	// https://host:port/edge/client/v1, sorted.
	Controllers []string `yaml:"controllers"`
}

// Load reads the endpoint file at path. A missing file loads as an empty File.
func Load(path string) (*File, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &File{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("endpoint file could not be read: %w", err)
	}

	f := &File{}
	if err := yaml.Unmarshal(content, f); err != nil {
		return nil, fmt.Errorf("endpoint file %s is not valid yaml: %w", path, err)
	}
	f.normalize()
	return f, nil
}

// Save writes f to path, replacing the file atomically.
func Save(path string, f *File) error {
	f.normalize()
	content, err := yaml.Marshal(f)
	if err != nil {
		return fmt.Errorf("endpoint file could not be encoded: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("endpoint file directory could not be created: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("endpoint file could not be written: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("endpoint file could not be written: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("endpoint file could not be written: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("endpoint file could not be replaced: %w", err)
	}
	return nil
}

// SetControllers replaces the controller list with urls and reports whether the list
// changed.
func (f *File) SetControllers(urls []*url.URL) bool {
	controllers := make([]string, 0, len(urls))
	for _, u := range urls {
		if u != nil {
			controllers = append(controllers, u.String())
		}
	}
	slices.Sort(controllers)
	controllers = slices.Compact(controllers)

	if slices.Equal(controllers, f.Controllers) {
		return false
	}
	f.Controllers = controllers
	return true
}

func (f *File) normalize() {
	slices.Sort(f.Controllers)
	f.Controllers = slices.Compact(f.Controllers)
}

// Tracker keeps an endpoint file current from the controller sets ziti.Contexts discover.
type Tracker struct {
	path string
	mu   sync.Mutex
}

// NewTracker builds a Tracker for the endpoint file at path.
func NewTracker(path string) *Tracker {
	return &Tracker{path: path}
}

// Track registers for the controller set zctx discovers after each authentication and
// rewrites the endpoint file whenever a controller is added or removed. It returns a function
// that stops tracking.
func (t *Tracker) Track(zctx ziti.Context) func() {
	listener := func(args ...any) {
		if len(args) < 2 {
			return
		}
		urls, ok := args[1].([]*url.URL)
		if !ok {
			return
		}
		if err := t.Update(urls); err != nil {
			pfxlog.Logger().WithError(err).Warn("endpoint file could not be updated")
		}
	}
	zctx.Events().AddListener(ziti.EventControllerUrlsUpdated, listener)
	return func() {
		zctx.Events().RemoveListener(ziti.EventControllerUrlsUpdated, listener)
	}
}

// Update replaces the controller list in the endpoint file with urls when it differs from
// what the file holds.
func (t *Tracker) Update(urls []*url.URL) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	f, err := Load(t.path)
	if err != nil {
		return err
	}
	if !f.SetControllers(urls) {
		return nil
	}

	pfxlog.Logger().WithField("controllers", f.Controllers).Info("controller set changed, updating endpoint file")
	return Save(t.path, f)
}

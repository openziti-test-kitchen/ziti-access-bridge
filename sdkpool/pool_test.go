package sdkpool

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openziti/sdk-golang/v2/ziti"
	"github.com/stretchr/testify/require"
)

type fakeContext struct {
	ziti.Context
	mu     sync.Mutex
	jwts   []string
	closed atomic.Int32
}

func (f *fakeContext) LoginWithJWT(jwt string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jwts = append(f.jwts, jwt)
}

func (f *fakeContext) Close() {
	f.closed.Add(1)
}

func (f *fakeContext) logins() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.jwts...)
}

func newTestPool(t *testing.T, opts Options) (*Pool, *atomic.Int32, *fakeContext) {
	t.Helper()
	opts.Controllers = []string{"https://ctrl.invalid/edge/client/v1"}
	pool, err := New(opts)
	require.NoError(t, err)

	calls := &atomic.Int32{}
	fake := &fakeContext{}
	pool.authenticate = func(string) (ziti.Context, error) {
		calls.Add(1)
		return fake, nil
	}
	return pool, calls, fake
}

func TestPool_Acquire(t *testing.T) {
	t.Run("first acquire authenticates and later acquires reuse", func(t *testing.T) {
		pool, calls, fake := newTestPool(t, Options{})
		exp := time.Now().Add(time.Hour)

		first, err := pool.Acquire("iss|alice", "jwt-1", exp)
		require.NoError(t, err)
		second, err := pool.Acquire("iss|alice", "jwt-1", exp)
		require.NoError(t, err)

		require.Same(t, fake, first)
		require.Same(t, first, second)
		require.EqualValues(t, 1, calls.Load(), "the same jwt must not re-authenticate")
		require.Empty(t, fake.logins(), "an unchanged jwt must not be handed to the context")
	})

	t.Run("a new jwt for the same requester is handed to the context", func(t *testing.T) {
		pool, calls, fake := newTestPool(t, Options{})
		exp := time.Now().Add(time.Hour)

		_, err := pool.Acquire("iss|alice", "jwt-1", exp)
		require.NoError(t, err)
		_, err = pool.Acquire("iss|alice", "jwt-2", exp.Add(time.Hour))
		require.NoError(t, err)

		require.EqualValues(t, 1, calls.Load(), "a rotated jwt must reuse the context")
		require.Equal(t, []string{"jwt-2"}, fake.logins())
	})

	t.Run("different requesters get different contexts", func(t *testing.T) {
		pool, calls, _ := newTestPool(t, Options{})
		exp := time.Now().Add(time.Hour)

		_, err := pool.Acquire("iss|alice", "jwt-a", exp)
		require.NoError(t, err)
		_, err = pool.Acquire("iss|bob", "jwt-b", exp)
		require.NoError(t, err)

		require.EqualValues(t, 2, calls.Load())
		require.Equal(t, 2, pool.Len())
	})

	t.Run("concurrent first acquires share one authentication", func(t *testing.T) {
		pool, calls, _ := newTestPool(t, Options{})
		release := make(chan struct{})
		pool.authenticate = func(string) (ziti.Context, error) {
			calls.Add(1)
			<-release
			return &fakeContext{}, nil
		}
		exp := time.Now().Add(time.Hour)

		var wg sync.WaitGroup
		results := make([]ziti.Context, 8)
		for i := range results {
			wg.Add(1)
			go func() {
				defer wg.Done()
				results[i], _ = pool.Acquire("iss|alice", "jwt-1", exp)
			}()
		}
		close(release)
		wg.Wait()

		require.EqualValues(t, 1, calls.Load())
		for _, r := range results {
			require.Same(t, results[0], r)
		}
	})

	t.Run("failed authentication is not cached", func(t *testing.T) {
		pool, calls, _ := newTestPool(t, Options{})
		pool.authenticate = func(string) (ziti.Context, error) {
			calls.Add(1)
			if calls.Load() == 1 {
				return nil, errors.New("controller rejected jwt")
			}
			return &fakeContext{}, nil
		}
		exp := time.Now().Add(time.Hour)

		_, firstErr := pool.Acquire("iss|alice", "jwt-1", exp)
		second, secondErr := pool.Acquire("iss|alice", "jwt-1", exp)

		require.Error(t, firstErr)
		require.NoError(t, secondErr, "the next acquire must retry after a failure")
		require.NotNil(t, second)
		require.EqualValues(t, 2, calls.Load())
		require.Equal(t, 1, pool.Len())
	})
}

func TestPool_Reap(t *testing.T) {
	t.Run("idle contexts are closed and evicted", func(t *testing.T) {
		var evicted []string
		pool, _, fake := newTestPool(t, Options{
			IdleWindow: time.Minute,
			Grace:      time.Hour,
			OnEvict:    func(key string) { evicted = append(evicted, key) },
		})
		exp := time.Now().Add(24 * time.Hour)
		_, err := pool.Acquire("iss|alice", "jwt-1", exp)
		require.NoError(t, err)

		pool.reap(time.Now().Add(2 * time.Minute))

		require.Equal(t, 0, pool.Len())
		require.EqualValues(t, 1, fake.closed.Load())
		require.Equal(t, []string{"iss|alice"}, evicted)
	})

	t.Run("contexts past the newest expiry plus grace are closed", func(t *testing.T) {
		pool, _, fake := newTestPool(t, Options{
			IdleWindow: 24 * time.Hour,
			Grace:      time.Minute,
		})
		exp := time.Now().Add(10 * time.Minute)
		_, err := pool.Acquire("iss|alice", "jwt-1", exp)
		require.NoError(t, err)

		pool.reap(exp.Add(2 * time.Minute))

		require.Equal(t, 0, pool.Len())
		require.EqualValues(t, 1, fake.closed.Load())
	})

	t.Run("a newer expiry extends the context's life", func(t *testing.T) {
		pool, _, fake := newTestPool(t, Options{
			IdleWindow: 24 * time.Hour,
			Grace:      time.Minute,
		})
		first := time.Now().Add(10 * time.Minute)
		second := first.Add(time.Hour)
		_, err := pool.Acquire("iss|alice", "jwt-1", first)
		require.NoError(t, err)
		_, err = pool.Acquire("iss|alice", "jwt-2", second)
		require.NoError(t, err)

		pool.reap(first.Add(2 * time.Minute))

		require.Equal(t, 1, pool.Len(), "the newer expiry must keep the context alive")
		require.EqualValues(t, 0, fake.closed.Load())
	})

	t.Run("live contexts survive a reap", func(t *testing.T) {
		pool, _, fake := newTestPool(t, Options{
			IdleWindow: time.Hour,
			Grace:      time.Hour,
		})
		_, err := pool.Acquire("iss|alice", "jwt-1", time.Now().Add(time.Hour))
		require.NoError(t, err)

		pool.reap(time.Now())

		require.Equal(t, 1, pool.Len())
		require.EqualValues(t, 0, fake.closed.Load())
	})
}

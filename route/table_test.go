package route

import (
	"errors"
	"testing"

	"github.com/openziti/edge-api/rest_model"
	"github.com/stretchr/testify/require"
)

func service(name string, body map[string]any) rest_model.ServiceDetail {
	return rest_model.ServiceDetail{
		BaseEntity: rest_model.BaseEntity{ID: &name},
		Name:       &name,
		Config:     map[string]map[string]any{ConfigType: body},
	}
}

func routes(pairs ...string) map[string]any {
	var list []any
	for i := 0; i+1 < len(pairs); i += 2 {
		list = append(list, map[string]any{"hostname": pairs[i], "pathPrefix": pairs[i+1]})
	}
	return map[string]any{"routes": list}
}

func TestTable_Resolve(t *testing.T) {
	t.Run("exact hostname match ignores port and case", func(t *testing.T) {
		table, err := NewTable([]rest_model.ServiceDetail{
			service("app", routes("App.Example.com", "/")),
		})
		require.NoError(t, err)

		match, err := table.Resolve("app.example.com:443", "/anything")

		require.NoError(t, err)
		require.Equal(t, "app", match.Service)
		require.Equal(t, "/", match.Route.PathPrefix)
	})

	t.Run("longest path prefix wins across services", func(t *testing.T) {
		table, err := NewTable([]rest_model.ServiceDetail{
			service("root", routes("app.example.com", "/")),
			service("api", routes("app.example.com", "/api")),
			service("api-v2", routes("app.example.com", "/api/v2")),
		})
		require.NoError(t, err)

		match, err := table.Resolve("app.example.com", "/api/v2/users")

		require.NoError(t, err)
		require.Equal(t, "api-v2", match.Service)
	})

	t.Run("path prefix matches on segment boundaries only", func(t *testing.T) {
		table, err := NewTable([]rest_model.ServiceDetail{
			service("root", routes("app.example.com", "/")),
			service("api", routes("app.example.com", "/api")),
		})
		require.NoError(t, err)

		match, err := table.Resolve("app.example.com", "/apix/users")

		require.NoError(t, err)
		require.Equal(t, "root", match.Service, "/apix must not match the /api prefix")
	})

	t.Run("hostname with no route is ErrNoRoute", func(t *testing.T) {
		table, err := NewTable([]rest_model.ServiceDetail{
			service("app", routes("app.example.com", "/")),
		})
		require.NoError(t, err)

		_, err = table.Resolve("other.example.com", "/")

		require.ErrorIs(t, err, ErrNoRoute)
	})

	t.Run("path outside every prefix is ErrNoRoute", func(t *testing.T) {
		table, err := NewTable([]rest_model.ServiceDetail{
			service("api", routes("app.example.com", "/api")),
		})
		require.NoError(t, err)

		_, err = table.Resolve("app.example.com", "/")

		require.ErrorIs(t, err, ErrNoRoute)
	})

	t.Run("same hostname and prefix on two services is ambiguous", func(t *testing.T) {
		table, err := NewTable([]rest_model.ServiceDetail{
			service("one", routes("app.example.com", "/api")),
			service("two", routes("app.example.com", "/api/")),
		})
		require.NoError(t, err)

		_, err = table.Resolve("app.example.com", "/api/users")

		var ambiguous *AmbiguousRouteError
		require.True(t, errors.As(err, &ambiguous), "expected AmbiguousRouteError, got %v", err)
		require.ElementsMatch(t, []string{"one", "two"}, ambiguous.Services)
		require.Equal(t, "/api", ambiguous.PathPrefix)
	})

	t.Run("services without a zab config are skipped", func(t *testing.T) {
		name := "plain"
		plain := rest_model.ServiceDetail{Name: &name, Config: map[string]map[string]any{}}
		table, err := NewTable([]rest_model.ServiceDetail{
			plain,
			service("app", routes("app.example.com", "/")),
		})
		require.NoError(t, err)

		match, err := table.Resolve("app.example.com", "/")

		require.NoError(t, err)
		require.Equal(t, "app", match.Service)
	})

	t.Run("a service with a bad zab config is skipped and reported", func(t *testing.T) {
		table, err := NewTable([]rest_model.ServiceDetail{
			service("broken", map[string]any{"routes": []any{}, "scheme": "ftp"}),
			service("app", routes("app.example.com", "/")),
		})

		require.Error(t, err, "the broken config must be reported")
		match, resolveErr := table.Resolve("app.example.com", "/")
		require.NoError(t, resolveErr, "the good service must still route")
		require.Equal(t, "app", match.Service)
	})
}

func TestParseConfig(t *testing.T) {
	t.Run("defaults apply", func(t *testing.T) {
		cfg, err := ParseConfig(map[string]any{
			"routes": []any{map[string]any{"hostname": "app.example.com"}},
		})

		require.NoError(t, err)
		require.Equal(t, SchemeHTTP, cfg.Scheme)
		require.Equal(t, "/", cfg.Routes[0].PathPrefix)
		require.True(t, cfg.VerifyTLS())
		require.False(t, cfg.StripPathPrefix)
		require.False(t, cfg.StripAccessProviderJWT)
	})

	t.Run("path prefix is normalized", func(t *testing.T) {
		cfg, err := ParseConfig(map[string]any{
			"routes": []any{map[string]any{"hostname": "app.example.com", "pathPrefix": "api/v1/"}},
		})

		require.NoError(t, err)
		require.Equal(t, "/api/v1", cfg.Routes[0].PathPrefix)
	})

	t.Run("tlsVerify false is honored", func(t *testing.T) {
		cfg, err := ParseConfig(map[string]any{
			"routes":    []any{map[string]any{"hostname": "app.example.com"}},
			"scheme":    "https",
			"tlsVerify": false,
		})

		require.NoError(t, err)
		require.Equal(t, SchemeHTTPS, cfg.Scheme)
		require.False(t, cfg.VerifyTLS())
	})

	t.Run("unknown scheme is rejected", func(t *testing.T) {
		_, err := ParseConfig(map[string]any{
			"routes": []any{map[string]any{"hostname": "app.example.com"}},
			"scheme": "ftp",
		})

		require.Error(t, err)
	})

	t.Run("no routes is rejected", func(t *testing.T) {
		_, err := ParseConfig(map[string]any{})

		require.Error(t, err)
	})

	t.Run("route without hostname is rejected", func(t *testing.T) {
		_, err := ParseConfig(map[string]any{
			"routes": []any{map[string]any{"pathPrefix": "/api"}},
		})

		require.Error(t, err)
	})
}

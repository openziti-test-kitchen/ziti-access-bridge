package route

import (
	"strings"
)

// ConfigTypePrefix opens the name of every ZAB config type. The rest of the name is the
// semantic version of the config's shape, as in zab-v1.0.0.
const ConfigTypePrefix = "zab-v"

// ConfigType is the name of the current ZAB config type, the one this build writes and
// reads first.
const ConfigType = ConfigTypePrefix + "1.0.0"

// ConfigTypeSpec describes one ZAB config type version: its OpenZiti config type name and
// the JSON schema the OpenZiti controller enforces on configs of that type.
type ConfigTypeSpec struct {
	Name    string
	Version string
	Schema  map[string]any
}

// ConfigTypes lists every ZAB config type version this build understands, newest first.
// Table reads configs of any listed type, and the zab agent registers each with the
// OpenZiti controller.
func ConfigTypes() []ConfigTypeSpec {
	return []ConfigTypeSpec{
		{
			Name:    ConfigType,
			Version: strings.TrimPrefix(ConfigType, ConfigTypePrefix),
			Schema:  schemaV1,
		},
	}
}

// ConfigTypeNames lists the names from ConfigTypes, for requesting them with an OpenZiti
// SDK context's services.
func ConfigTypeNames() []string {
	specs := ConfigTypes()
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		names = append(names, spec.Name)
	}
	return names
}

// ConfigTarget is the OpenZiti config type target every ZAB config type declares: the
// configs attach to OpenZiti services.
const ConfigTarget = "service"

var schemaV1 = map[string]any{
	"$schema":              "http://json-schema.org/draft-07/schema",
	"type":                 "object",
	"required":             []any{"routes"},
	"additionalProperties": false,
	"properties": map[string]any{
		"routes": map[string]any{
			"type":     "array",
			"minItems": 1,
			"items": map[string]any{
				"type":                 "object",
				"required":             []any{"hostname"},
				"additionalProperties": false,
				"properties": map[string]any{
					"hostname":   map[string]any{"type": "string", "minLength": 1},
					"pathPrefix": map[string]any{"type": "string"},
				},
			},
		},
		"stripPathPrefix":        map[string]any{"type": "boolean"},
		"scheme":                 map[string]any{"type": "string", "enum": []any{SchemeHTTP, SchemeHTTPS}},
		"tlsVerify":              map[string]any{"type": "boolean"},
		"hostHeader":             map[string]any{"type": "string"},
		"stripAccessProviderJwt": map[string]any{"type": "boolean"},
	},
}

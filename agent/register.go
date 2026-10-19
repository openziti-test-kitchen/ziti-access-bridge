package agent

import (
	"context"
	"fmt"

	mgmtconfig "github.com/openziti/edge-api/rest_management_api_client/config"
	"github.com/openziti/edge-api/rest_model"
	"github.com/openziti/edge-api/rest_util"
	"github.com/openziti/identity"
	edgeapis "github.com/openziti/sdk-golang/v2/edge-apis"

	"github.com/openziti-test-kitchen/ziti-access-bridge/config"
	"github.com/openziti-test-kitchen/ziti-access-bridge/endpoints"
	"github.com/openziti-test-kitchen/ziti-access-bridge/route"
)

// Action is what RegisterConfigTypes did for one config type.
type Action string

// The actions RegisterConfigTypes reports.
const (
	ActionCreated   Action = "created"
	ActionUpdated   Action = "updated"
	ActionUnchanged Action = "unchanged"
)

// Registration reports what RegisterConfigTypes did for one ZAB config type.
type Registration struct {
	Name   string
	ID     string
	Action Action
}

// RegisterConfigTypes creates every ZAB config type this build supports on the OpenZiti
// controller, or updates the schema of one already registered, using ZAB's own identity
// against the Edge Management API.
func RegisterConfigTypes(ctx context.Context, cfg *config.File) ([]Registration, error) {
	id, err := identity.LoadIdentity(cfg.Identity)
	if err != nil {
		return nil, fmt.Errorf("zab identity could not be loaded, enroll first: %w", err)
	}

	known, err := endpoints.Load(cfg.EndpointFile)
	if err != nil {
		return nil, err
	}

	mgmt, err := managementClient(cfg, known.Controllers, id.CA())
	if err != nil {
		return nil, err
	}

	var results []Registration
	for _, spec := range route.ConfigTypes() {
		reg, err := registerConfigType(ctx, mgmt, spec)
		if err != nil {
			return results, err
		}
		results = append(results, reg)
	}
	return results, nil
}

func registerConfigType(ctx context.Context, mgmt *edgeapis.ManagementApiClient, spec route.ConfigTypeSpec) (Registration, error) {
	existing, err := findConfigType(ctx, mgmt, spec.Name)
	if err != nil {
		return Registration{}, err
	}

	if existing == nil {
		name := spec.Name
		target := route.ConfigTarget
		params := mgmtconfig.NewCreateConfigTypeParams().WithContext(ctx).WithConfigType(&rest_model.ConfigTypeCreate{
			Name:   &name,
			Schema: spec.Schema,
			Target: &target,
		})
		resp, err := mgmt.API.Config.CreateConfigType(params, nil)
		if err != nil {
			return Registration{}, fmt.Errorf("config type %s could not be created: %w", spec.Name, rest_util.WrapErr(err))
		}
		return Registration{Name: spec.Name, ID: resp.Payload.Data.ID, Action: ActionCreated}, nil
	}

	if schemasEqual(existing.Schema, spec.Schema) {
		return Registration{Name: spec.Name, ID: *existing.ID, Action: ActionUnchanged}, nil
	}

	params := mgmtconfig.NewPatchConfigTypeParams().WithContext(ctx).WithID(*existing.ID).WithConfigType(&rest_model.ConfigTypePatch{
		Schema: spec.Schema,
	})
	if _, err := mgmt.API.Config.PatchConfigType(params, nil); err != nil {
		return Registration{}, fmt.Errorf("config type %s schema could not be updated: %w", spec.Name, rest_util.WrapErr(err))
	}
	return Registration{Name: spec.Name, ID: *existing.ID, Action: ActionUpdated}, nil
}

// findConfigType returns the registered config type named name, or nil when none is.
func findConfigType(ctx context.Context, mgmt *edgeapis.ManagementApiClient, name string) (*rest_model.ConfigTypeDetail, error) {
	filter := fmt.Sprintf(`name = "%s"`, name)
	params := mgmtconfig.NewListConfigTypesParams().WithContext(ctx).WithFilter(&filter)
	resp, err := mgmt.API.Config.ListConfigTypes(params, nil)
	if err != nil {
		return nil, fmt.Errorf("config types could not be listed: %w", rest_util.WrapErr(err))
	}
	for _, ct := range resp.Payload.Data {
		if ct != nil && ct.Name != nil && *ct.Name == name {
			return ct, nil
		}
	}
	return nil, nil
}

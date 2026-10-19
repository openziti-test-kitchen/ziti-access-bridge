package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	mgmtconfig "github.com/openziti/edge-api/rest_management_api_client/config"
	mgmtservice "github.com/openziti/edge-api/rest_management_api_client/service"
	"github.com/openziti/edge-api/rest_model"
	"github.com/openziti/edge-api/rest_util"
	"github.com/openziti/identity"
	edgeapis "github.com/openziti/sdk-golang/v2/edge-apis"

	"github.com/openziti-test-kitchen/ziti-access-bridge/config"
	"github.com/openziti-test-kitchen/ziti-access-bridge/endpoints"
	"github.com/openziti-test-kitchen/ziti-access-bridge/route"
)

const listPageSize = 500

// ServiceConfig is one OpenZiti service and the ZAB config attached to it.
type ServiceConfig struct {
	Service    string
	ServiceID  string
	ConfigType string
	Config     *route.Config
}

// ServiceChange reports what SetServiceConfig did.
type ServiceChange struct {
	Service        string
	ServiceID      string
	ServiceCreated bool
	ConfigName     string
	ConfigID       string
	ConfigAction   Action
	Attached       bool
}

// ConfigName returns the name of the ZAB config SetServiceConfig attaches to the named
// service: the service name, a dot, and the current config type name.
func ConfigName(service string) string {
	return service + "." + route.ConfigType
}

// SetServiceConfig attaches rc to the named OpenZiti service as a config of the current ZAB
// config type, creating the service when it does not exist and creating or updating the
// config as needed. The service is created bare, with no policies and no other configs.
// Everything else about a service's life stays with the OpenZiti CLI.
func SetServiceConfig(ctx context.Context, cfg *config.File, service string, rc *route.Config) (*ServiceChange, error) {
	if err := rc.Normalize(); err != nil {
		return nil, err
	}

	mgmt, err := adminClient(cfg)
	if err != nil {
		return nil, err
	}

	configType, err := findConfigType(ctx, mgmt, route.ConfigType)
	if err != nil {
		return nil, err
	}
	if configType == nil {
		return nil, fmt.Errorf("config type %s is not registered, run zab agent register-configs", route.ConfigType)
	}

	change := &ServiceChange{Service: service, ConfigName: ConfigName(service)}

	svc, err := findService(ctx, mgmt, service)
	if err != nil {
		return nil, err
	}
	if svc == nil {
		svc, err = createService(ctx, mgmt, service)
		if err != nil {
			return nil, err
		}
		change.ServiceCreated = true
	}
	change.ServiceID = *svc.ID

	existing, err := findConfig(ctx, mgmt, change.ConfigName)
	if err != nil {
		return nil, err
	}
	switch {
	case existing == nil:
		id, err := createConfig(ctx, mgmt, change.ConfigName, *configType.ID, rc)
		if err != nil {
			return nil, err
		}
		change.ConfigID = id
		change.ConfigAction = ActionCreated
	case schemasEqual(existing.Data, rc):
		change.ConfigID = *existing.ID
		change.ConfigAction = ActionUnchanged
	default:
		if err := patchConfig(ctx, mgmt, *existing.ID, rc); err != nil {
			return nil, err
		}
		change.ConfigID = *existing.ID
		change.ConfigAction = ActionUpdated
	}

	if !slices.Contains(svc.Configs, change.ConfigID) {
		if err := attachConfig(ctx, mgmt, *svc.ID, append(svc.Configs, change.ConfigID)); err != nil {
			return nil, err
		}
		change.Attached = true
	}

	return change, nil
}

// ShowServiceConfig returns the ZAB config attached to the named OpenZiti service, or an
// error when the service does not exist or carries none.
func ShowServiceConfig(ctx context.Context, cfg *config.File, service string) (*ServiceConfig, error) {
	mgmt, err := adminClient(cfg)
	if err != nil {
		return nil, err
	}

	svc, err := findService(ctx, mgmt, service)
	if err != nil {
		return nil, err
	}
	if svc == nil {
		return nil, fmt.Errorf("openziti service %q does not exist", service)
	}

	sc, ok, err := serviceConfig(svc)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("openziti service %q carries no zab config", service)
	}
	return sc, nil
}

// ListServiceConfigs returns every OpenZiti service that carries a ZAB config.
func ListServiceConfigs(ctx context.Context, cfg *config.File) ([]ServiceConfig, error) {
	mgmt, err := adminClient(cfg)
	if err != nil {
		return nil, err
	}

	var out []ServiceConfig
	var offset int64
	for {
		limit := int64(listPageSize)
		params := mgmtservice.NewListServicesParams().WithContext(ctx).WithLimit(&limit).WithOffset(&offset).WithConfigTypes(route.ConfigTypeNames())
		resp, err := mgmt.API.Service.ListServices(params, nil)
		if err != nil {
			return nil, fmt.Errorf("services could not be listed: %w", rest_util.WrapErr(err))
		}
		for _, svc := range resp.Payload.Data {
			sc, ok, err := serviceConfig(svc)
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, *sc)
			}
		}
		if len(resp.Payload.Data) < listPageSize {
			return out, nil
		}
		offset += int64(len(resp.Payload.Data))
	}
}

func serviceConfig(svc *rest_model.ServiceDetail) (*ServiceConfig, bool, error) {
	for _, spec := range route.ConfigTypes() {
		body, ok := svc.Config[spec.Name]
		if !ok {
			continue
		}
		rc, err := route.ParseConfig(body)
		if err != nil {
			return nil, false, fmt.Errorf("openziti service %q: %w", *svc.Name, err)
		}
		return &ServiceConfig{
			Service:    *svc.Name,
			ServiceID:  *svc.ID,
			ConfigType: spec.Name,
			Config:     rc,
		}, true, nil
	}
	return nil, false, nil
}

func adminClient(cfg *config.File) (*edgeapis.ManagementApiClient, error) {
	id, err := identity.LoadIdentity(cfg.Identity)
	if err != nil {
		return nil, fmt.Errorf("zab identity could not be loaded, enroll first: %w", err)
	}
	known, err := endpoints.Load(cfg.EndpointFile)
	if err != nil {
		return nil, err
	}
	return managementClient(cfg, known.Controllers, id.CA())
}

func findService(ctx context.Context, mgmt *edgeapis.ManagementApiClient, name string) (*rest_model.ServiceDetail, error) {
	filter := fmt.Sprintf(`name = "%s"`, escapeFilter(name))
	params := mgmtservice.NewListServicesParams().WithContext(ctx).WithFilter(&filter).WithConfigTypes(route.ConfigTypeNames())
	resp, err := mgmt.API.Service.ListServices(params, nil)
	if err != nil {
		return nil, fmt.Errorf("services could not be listed: %w", rest_util.WrapErr(err))
	}
	for _, svc := range resp.Payload.Data {
		if svc != nil && svc.Name != nil && *svc.Name == name {
			return svc, nil
		}
	}
	return nil, nil
}

func createService(ctx context.Context, mgmt *edgeapis.ManagementApiClient, name string) (*rest_model.ServiceDetail, error) {
	encryption := true
	params := mgmtservice.NewCreateServiceParams().WithContext(ctx).WithService(&rest_model.ServiceCreate{
		Name:               &name,
		EncryptionRequired: &encryption,
		Configs:            []string{},
		RoleAttributes:     []string{},
	})
	resp, err := mgmt.API.Service.CreateService(params, nil)
	if err != nil {
		return nil, fmt.Errorf("openziti service %q could not be created: %w", name, rest_util.WrapErr(err))
	}
	id := resp.Payload.Data.ID
	return &rest_model.ServiceDetail{
		BaseEntity: rest_model.BaseEntity{ID: &id},
		Name:       &name,
		Configs:    []string{},
	}, nil
}

func findConfig(ctx context.Context, mgmt *edgeapis.ManagementApiClient, name string) (*rest_model.ConfigDetail, error) {
	filter := fmt.Sprintf(`name = "%s"`, escapeFilter(name))
	params := mgmtconfig.NewListConfigsParams().WithContext(ctx).WithFilter(&filter)
	resp, err := mgmt.API.Config.ListConfigs(params, nil)
	if err != nil {
		return nil, fmt.Errorf("configs could not be listed: %w", rest_util.WrapErr(err))
	}
	for _, c := range resp.Payload.Data {
		if c != nil && c.Name != nil && *c.Name == name {
			return c, nil
		}
	}
	return nil, nil
}

func createConfig(ctx context.Context, mgmt *edgeapis.ManagementApiClient, name, configTypeID string, rc *route.Config) (string, error) {
	data, err := configData(rc)
	if err != nil {
		return "", err
	}
	params := mgmtconfig.NewCreateConfigParams().WithContext(ctx).WithConfig(&rest_model.ConfigCreate{
		Name:         &name,
		ConfigTypeID: &configTypeID,
		Data:         data,
	})
	resp, err := mgmt.API.Config.CreateConfig(params, nil)
	if err != nil {
		return "", fmt.Errorf("config %q could not be created: %w", name, rest_util.WrapErr(err))
	}
	return resp.Payload.Data.ID, nil
}

func patchConfig(ctx context.Context, mgmt *edgeapis.ManagementApiClient, id string, rc *route.Config) error {
	data, err := configData(rc)
	if err != nil {
		return err
	}
	params := mgmtconfig.NewPatchConfigParams().WithContext(ctx).WithID(id).WithConfig(&rest_model.ConfigPatch{Data: data})
	if _, err := mgmt.API.Config.PatchConfig(params, nil); err != nil {
		return fmt.Errorf("config %s could not be updated: %w", id, rest_util.WrapErr(err))
	}
	return nil
}

func attachConfig(ctx context.Context, mgmt *edgeapis.ManagementApiClient, serviceID string, configs []string) error {
	params := mgmtservice.NewPatchServiceParams().WithContext(ctx).WithID(serviceID).WithService(&rest_model.ServicePatch{
		Configs: configs,
	})
	if _, err := mgmt.API.Service.PatchService(params, nil); err != nil {
		return fmt.Errorf("config could not be attached to service %s: %w", serviceID, rest_util.WrapErr(err))
	}
	return nil
}

// configData turns rc into the plain map the controller stores, so what is sent matches
// what ServiceDetail.Config hands back.
func configData(rc *route.Config) (map[string]any, error) {
	raw, err := json.Marshal(rc)
	if err != nil {
		return nil, fmt.Errorf("zab config could not be encoded: %w", err)
	}
	data := map[string]any{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("zab config could not be decoded: %w", err)
	}
	return data, nil
}

func escapeFilter(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}

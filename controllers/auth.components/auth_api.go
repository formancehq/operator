package authcomponents

import (
	"context"

	"github.com/numary/auth/authclient"
)

type AuthServerAPI interface {
	DeleteScope(ctx context.Context, id string) error
	CreateScope(ctx context.Context, label string) (string, error)
	UpdateScope(ctx context.Context, id string, label string) error
	ListScopes(ctx context.Context) (Filterable[authclient.Scope], error)
}

type defaultServerApi struct {
	API *authclient.APIClient
}

func (d *defaultServerApi) DeleteScope(ctx context.Context, id string) error {
	_, err := d.API.DefaultApi.
		DeleteScope(ctx, id).
		Execute()
	return err
}

func (d *defaultServerApi) CreateScope(ctx context.Context, label string) (string, error) {
	ret, _, err := d.API.DefaultApi.CreateScope(ctx).Body(authclient.ScopeOptions{
		Label: label,
	}).Execute()
	if err != nil {
		return "", err
	}
	return ret.Data.Id, nil
}

func (d *defaultServerApi) UpdateScope(ctx context.Context, id string, label string) error {
	_, _, err := d.API.DefaultApi.UpdateScope(ctx, id).Body(authclient.ScopeOptions{
		Label: label,
	}).Execute()
	return err
}

func (d *defaultServerApi) ListScopes(ctx context.Context) (Filterable[authclient.Scope], error) {
	scopes, _, err := d.API.DefaultApi.ListScopes(ctx).Execute()
	if err != nil {
		return nil, err
	}
	return scopes.Data, nil
}

var _ AuthServerAPI = &defaultServerApi{}

func NewDefaultServerApi(api *authclient.APIClient) *defaultServerApi {
	return &defaultServerApi{
		API: api,
	}
}

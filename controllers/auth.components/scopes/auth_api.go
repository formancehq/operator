package scopes

import (
	"context"
	"errors"
	"net/http"

	"github.com/numary/auth/authclient"
	"github.com/numary/formance-operator/pkg/collectionutil"
)

var ErrNotFound = errors.New("not found")

type ScopeAPI interface {
	DeleteScope(ctx context.Context, id string) error
	ReadScope(ctx context.Context, id string) (*authclient.Scope, error)
	CreateScope(ctx context.Context, label string, metadata map[string]string) (*authclient.Scope, error)
	UpdateScope(ctx context.Context, id string, label string, metadata map[string]string) error
	ListScopes(ctx context.Context) (collectionutil.Array[authclient.Scope], error)
	ReadScopeByMetadata(ctx context.Context, metadata map[string]string) (*authclient.Scope, error)
	AddTransientScope(ctx context.Context, scope, transientScope string) error
	RemoveTransientScope(ctx context.Context, scope, transientScope string) error
}

type defaultServerApi struct {
	API *authclient.APIClient
}

func (d *defaultServerApi) AddTransientScope(ctx context.Context, scope, transientScope string) error {
	httpResponse, err := d.API.DefaultApi.AddTransientScope(ctx, scope, transientScope).Execute()
	if err != nil && httpResponse.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	return err
}

func (d *defaultServerApi) RemoveTransientScope(ctx context.Context, scope, transientScope string) error {
	httpResponse, err := d.API.DefaultApi.DeleteTransientScope(ctx, scope, transientScope).Execute()
	if err != nil && httpResponse.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	return err
}

func (d *defaultServerApi) ReadScopeByMetadata(ctx context.Context, metadata map[string]string) (*authclient.Scope, error) {
	allScopes, err := d.ListScopes(ctx)
	if err != nil {
		return nil, err
	}

l:
	for _, scope := range allScopes {
		if scope.Metadata == nil {
			continue
		}
		for k, v := range *scope.Metadata {
			if metadata[k] != v {
				continue l
			}
		}
		return &scope, nil
	}
	return nil, ErrNotFound
}

func (d *defaultServerApi) ReadScope(ctx context.Context, id string) (*authclient.Scope, error) {
	ret, httpResponse, err := d.API.DefaultApi.
		ReadScope(ctx, id).
		Execute()
	if err != nil {
		if httpResponse.StatusCode == http.StatusNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return ret.Data, nil
}

func (d *defaultServerApi) DeleteScope(ctx context.Context, id string) error {
	httpResponse, err := d.API.DefaultApi.
		DeleteScope(ctx, id).
		Execute()
	if err != nil && httpResponse.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	return err
}

func (d *defaultServerApi) CreateScope(ctx context.Context, label string, metadata map[string]string) (*authclient.Scope, error) {
	ret, httpResponse, err := d.API.DefaultApi.CreateScope(ctx).Body(authclient.ScopeOptions{
		Label:    label,
		Metadata: &metadata,
	}).Execute()
	if err != nil {
		if httpResponse.StatusCode == http.StatusNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return ret.Data, nil
}

func (d *defaultServerApi) UpdateScope(ctx context.Context, id string, label string, metadata map[string]string) error {
	_, httpResponse, err := d.API.DefaultApi.UpdateScope(ctx, id).Body(authclient.ScopeOptions{
		Label:    label,
		Metadata: &metadata,
	}).Execute()
	if err != nil && httpResponse.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	return err
}

func (d *defaultServerApi) ListScopes(ctx context.Context) (collectionutil.Array[authclient.Scope], error) {
	scopes, _, err := d.API.DefaultApi.ListScopes(ctx).Execute()
	if err != nil {
		return nil, err
	}
	return scopes.Data, nil
}

var _ ScopeAPI = &defaultServerApi{}

func NewDefaultServerApi(api *authclient.APIClient) *defaultServerApi {
	return &defaultServerApi{
		API: api,
	}
}

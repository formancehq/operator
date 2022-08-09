package scopes

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
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

type inMemoryScopeApi struct {
	scopes map[string]*authclient.Scope
}

func (i *inMemoryScopeApi) AddTransientScope(ctx context.Context, scope, transientScope string) error {
	firstScope, ok := i.scopes[scope]
	if !ok {
		return ErrNotFound
	}
	_, ok = i.scopes[transientScope]
	if !ok {
		return ErrNotFound
	}
	firstScope.Transient = append(firstScope.Transient, transientScope)
	return nil
}

func (i *inMemoryScopeApi) RemoveTransientScope(ctx context.Context, scope, transientScope string) error {
	firstScope, ok := i.scopes[scope]
	if !ok {
		return ErrNotFound
	}
	_, ok = i.scopes[transientScope]
	if !ok {
		return ErrNotFound
	}
	firstScope.Transient = collectionutil.Array[string](firstScope.Transient).Filter(func(t string) bool {
		return t != transientScope
	})
	return nil
}

func (i *inMemoryScopeApi) ReadScope(ctx context.Context, id string) (*authclient.Scope, error) {
	s, ok := i.scopes[id]
	if !ok {
		return nil, ErrNotFound
	}
	return s, nil
}

func (i *inMemoryScopeApi) ReadScopeByMetadata(ctx context.Context, metadata map[string]string) (*authclient.Scope, error) {
	allScopes, err := i.ListScopes(ctx)
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

func (i *inMemoryScopeApi) DeleteScope(ctx context.Context, id string) error {
	_, ok := i.scopes[id]
	if !ok {
		return ErrNotFound
	}
	delete(i.scopes, id)
	return nil
}

func (i *inMemoryScopeApi) CreateScope(ctx context.Context, label string, metadata map[string]string) (*authclient.Scope, error) {
	id := uuid.NewString()
	i.scopes[id] = &authclient.Scope{
		Label:    label,
		Id:       id,
		Metadata: &metadata,
	}
	return i.scopes[id], nil
}

func (i *inMemoryScopeApi) UpdateScope(ctx context.Context, id string, label string, metadata map[string]string) error {
	i.scopes[id].Label = label
	i.scopes[id].Metadata = &metadata
	return nil
}

func (i *inMemoryScopeApi) ListScopes(ctx context.Context) (collectionutil.Array[authclient.Scope], error) {
	ret := collectionutil.Array[authclient.Scope]{}
	for _, scope := range i.scopes {
		ret = append(ret, *scope)
	}
	return ret, nil
}

func (i *inMemoryScopeApi) reset() {
	i.scopes = map[string]*authclient.Scope{}
}

func newInMemoryScopeApi() *inMemoryScopeApi {
	return &inMemoryScopeApi{
		scopes: map[string]*authclient.Scope{},
	}
}

var _ ScopeAPI = (*inMemoryScopeApi)(nil)

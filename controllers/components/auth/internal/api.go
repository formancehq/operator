package internal

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/google/uuid"
	"github.com/numary/auth/authclient"
	. "github.com/numary/formance-operator/internal/collectionutil"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type API interface {
	DeleteScope(ctx context.Context, id string) error
	ReadScope(ctx context.Context, id string) (*authclient.Scope, error)
	CreateScope(ctx context.Context, label string, metadata map[string]string) (*authclient.Scope, error)
	UpdateScope(ctx context.Context, id string, label string, metadata map[string]string) error
	ListScopes(ctx context.Context) (Array[authclient.Scope], error)
	ReadScopeByMetadata(ctx context.Context, metadata map[string]string) (*authclient.Scope, error)
	AddTransientScope(ctx context.Context, scope, transientScope string) error
	RemoveTransientScope(ctx context.Context, scope, transientScope string) error
	CreateClient(ctx context.Context, options authclient.ClientOptions) (*authclient.Client, error)
	UpdateClient(ctx context.Context, id string, options authclient.ClientOptions) error
	DeleteClient(ctx context.Context, id string) error
	ReadClient(ctx context.Context, id string) (*authclient.Client, error)
	ReadClientByMetadata(ctx context.Context, metadata map[string]string) (*authclient.Client, error)
	AddScopeToClient(ctx context.Context, clientId, scopeId string) error
	DeleteScopeFromClient(ctx context.Context, clientId, scopeId string) error
}

var ErrNotFound = errors.New("not found")

type defaultApi struct {
	API *authclient.APIClient
}

func (d *defaultApi) AddTransientScope(ctx context.Context, scope, transientScope string) error {
	httpResponse, err := d.API.DefaultApi.AddTransientScope(ctx, scope, transientScope).Execute()
	if err != nil && httpResponse != nil && httpResponse.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	return ConvertError(err)
}

func (d *defaultApi) RemoveTransientScope(ctx context.Context, scope, transientScope string) error {
	httpResponse, err := d.API.DefaultApi.DeleteTransientScope(ctx, scope, transientScope).Execute()
	if err != nil && httpResponse != nil && httpResponse.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	return ConvertError(err)
}

func (d *defaultApi) ReadScopeByMetadata(ctx context.Context, metadata map[string]string) (*authclient.Scope, error) {
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

func (d *defaultApi) ReadScope(ctx context.Context, id string) (*authclient.Scope, error) {
	ret, httpResponse, err := d.API.DefaultApi.
		ReadScope(ctx, id).
		Execute()
	if err != nil {
		if httpResponse != nil && httpResponse.StatusCode == http.StatusNotFound {
			return nil, ErrNotFound
		}
		return nil, ConvertError(err)
	}
	return ret.Data, nil
}

func (d *defaultApi) DeleteScope(ctx context.Context, id string) error {
	httpResponse, err := d.API.DefaultApi.
		DeleteScope(ctx, id).
		Execute()
	if err != nil && httpResponse != nil && httpResponse.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	return ConvertError(err)
}

func (d *defaultApi) CreateScope(ctx context.Context, label string, metadata map[string]string) (*authclient.Scope, error) {
	ret, httpResponse, err := d.API.DefaultApi.CreateScope(ctx).Body(authclient.ScopeOptions{
		Label:    label,
		Metadata: &metadata,
	}).Execute()
	if err != nil {
		if httpResponse != nil && httpResponse.StatusCode == http.StatusNotFound {
			return nil, ErrNotFound
		}
		return nil, ConvertError(err)
	}
	return ret.Data, nil
}

func (d *defaultApi) UpdateScope(ctx context.Context, id string, label string, metadata map[string]string) error {
	_, httpResponse, err := d.API.DefaultApi.UpdateScope(ctx, id).Body(authclient.ScopeOptions{
		Label:    label,
		Metadata: &metadata,
	}).Execute()
	if err != nil && httpResponse != nil && httpResponse.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	return ConvertError(err)
}

func (d *defaultApi) ListScopes(ctx context.Context) (Array[authclient.Scope], error) {
	scopes, _, err := d.API.DefaultApi.ListScopes(ctx).Execute()
	if err != nil {
		return nil, ConvertError(err)
	}
	return scopes.Data, nil
}

func (d *defaultApi) CreateClient(ctx context.Context, options authclient.ClientOptions) (*authclient.Client, error) {
	ret, _, err := d.API.DefaultApi.CreateClient(ctx).Body(options).Execute()
	if err != nil {
		return nil, ConvertError(err)
	}
	return ret.Data, nil
}

func (d *defaultApi) UpdateClient(ctx context.Context, id string, options authclient.ClientOptions) error {
	_, httpResponse, err := d.API.DefaultApi.UpdateClient(ctx, id).Body(options).Execute()
	if err != nil {
		if httpResponse != nil && httpResponse.StatusCode == http.StatusNotFound {
			return ErrNotFound
		}
		return ConvertError(err)
	}
	return nil
}

func (d *defaultApi) DeleteClient(ctx context.Context, id string) error {
	httpResponse, err := d.API.DefaultApi.DeleteClient(ctx, id).Execute()
	if err != nil {
		if httpResponse != nil && httpResponse.StatusCode == http.StatusNotFound {
			return ErrNotFound
		}
		return ConvertError(err)
	}
	return nil
}

func (d *defaultApi) ReadClient(ctx context.Context, id string) (*authclient.Client, error) {
	ret, httpResponse, err := d.API.DefaultApi.ReadClient(ctx, id).Execute()
	if err != nil {
		if httpResponse != nil && httpResponse.StatusCode == http.StatusNotFound {
			return nil, ErrNotFound
		}
		return nil, ConvertError(err)
	}
	return ret.Data, nil
}

func (d *defaultApi) ReadClientByMetadata(ctx context.Context, metadata map[string]string) (*authclient.Client, error) {
	clients, _, err := d.API.DefaultApi.ListClients(ctx).Execute()
	if err != nil {
		return nil, err
	}
l:
	for _, client := range clients.Data {
		if client.Metadata == nil {
			continue
		}
		for k, v := range metadata {
			if (*client.Metadata)[k] != v {
				continue l
			}
		}
		return &client, nil
	}
	return nil, ErrNotFound
}

func (d *defaultApi) AddScopeToClient(ctx context.Context, clientId, scopeId string) error {
	httpResponse, err := d.API.DefaultApi.AddScopeToClient(ctx, clientId, scopeId).Execute()
	if err != nil {
		if httpResponse != nil && httpResponse.StatusCode == http.StatusNotFound {
			return ErrNotFound
		}
		return ConvertError(err)
	}
	return nil
}

func (d *defaultApi) DeleteScopeFromClient(ctx context.Context, clientId, scopeId string) error {
	httpResponse, err := d.API.DefaultApi.DeleteScopeFromClient(ctx, clientId, scopeId).Execute()
	if err != nil {
		if httpResponse != nil && httpResponse.StatusCode == http.StatusNotFound {
			return ErrNotFound
		}
		return ConvertError(err)
	}
	return nil
}

var _ API = &defaultApi{}

func NewDefaultServerApi(api *authclient.APIClient) *defaultApi {
	return &defaultApi{
		API: api,
	}
}

type InMemoryApi struct {
	scopes  map[string]*authclient.Scope
	clients map[string]*authclient.Client
	lock    sync.Mutex
}

func (i *InMemoryApi) Clients() map[string]*authclient.Client {
	i.lock.Lock()
	defer i.lock.Unlock()
	return i.clients
}

func (i *InMemoryApi) Scopes() map[string]*authclient.Scope {
	i.lock.Lock()
	defer i.lock.Unlock()
	return i.scopes
}

func (i *InMemoryApi) Client(name string) *authclient.Client {
	i.lock.Lock()
	defer i.lock.Unlock()
	return i.clients[name]
}

func (i *InMemoryApi) Scope(name string) *authclient.Scope {
	i.lock.Lock()
	defer i.lock.Unlock()
	return i.scopes[name]
}

func (i *InMemoryApi) AddTransientScope(ctx context.Context, scope, transientScope string) error {
	i.lock.Lock()
	defer i.lock.Unlock()
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

func (i *InMemoryApi) RemoveTransientScope(ctx context.Context, scope, transientScope string) error {
	i.lock.Lock()
	defer i.lock.Unlock()
	firstScope, ok := i.scopes[scope]
	if !ok {
		return ErrNotFound
	}
	_, ok = i.scopes[transientScope]
	if !ok {
		return ErrNotFound
	}
	firstScope.Transient = Array[string](firstScope.Transient).Filter(func(t string) bool {
		return t != transientScope
	})
	return nil
}

func (i *InMemoryApi) ReadScope(ctx context.Context, id string) (*authclient.Scope, error) {
	i.lock.Lock()
	defer i.lock.Unlock()
	s, ok := i.scopes[id]
	if !ok {
		return nil, ErrNotFound
	}
	return s, nil
}

func (i *InMemoryApi) ReadScopeByMetadata(ctx context.Context, metadata map[string]string) (*authclient.Scope, error) {
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

func (i *InMemoryApi) DeleteScope(ctx context.Context, id string) error {
	i.lock.Lock()
	defer i.lock.Unlock()
	_, ok := i.scopes[id]
	if !ok {
		return ErrNotFound
	}
	delete(i.scopes, id)
	return nil
}

func (i *InMemoryApi) CreateScope(ctx context.Context, label string, metadata map[string]string) (*authclient.Scope, error) {
	i.lock.Lock()
	defer i.lock.Unlock()
	id := uuid.NewString()
	i.scopes[id] = &authclient.Scope{
		Label:    label,
		Id:       id,
		Metadata: &metadata,
	}
	return i.scopes[id], nil
}

func (i *InMemoryApi) UpdateScope(ctx context.Context, id string, label string, metadata map[string]string) error {
	i.lock.Lock()
	defer i.lock.Unlock()
	i.scopes[id].Label = label
	i.scopes[id].Metadata = &metadata
	return nil
}

func (i *InMemoryApi) ListScopes(ctx context.Context) (Array[authclient.Scope], error) {
	i.lock.Lock()
	defer i.lock.Unlock()
	ret := Array[authclient.Scope]{}
	for _, scope := range i.scopes {
		ret = append(ret, *scope)
	}
	return ret, nil
}

func (i *InMemoryApi) CreateClient(ctx context.Context, options authclient.ClientOptions) (*authclient.Client, error) {
	i.lock.Lock()
	defer i.lock.Unlock()
	id := uuid.NewString()
	i.clients[id] = &authclient.Client{
		Public:                 options.Public,
		RedirectUris:           options.RedirectUris,
		Description:            options.Description,
		Name:                   options.Name,
		PostLogoutRedirectUris: options.PostLogoutRedirectUris,
		Metadata:               options.Metadata,
		Id:                     id,
		Scopes:                 []string{},
	}
	return i.clients[id], nil
}

func (i *InMemoryApi) UpdateClient(ctx context.Context, id string, options authclient.ClientOptions) error {
	i.lock.Lock()
	defer i.lock.Unlock()
	_, ok := i.clients[id]
	if !ok {
		return ErrNotFound
	}
	i.clients[id].Public = options.Public
	i.clients[id].RedirectUris = options.RedirectUris
	i.clients[id].Description = options.Description
	i.clients[id].Name = options.Name
	i.clients[id].PostLogoutRedirectUris = options.PostLogoutRedirectUris
	i.clients[id].Metadata = options.Metadata
	return nil
}

func (i *InMemoryApi) DeleteClient(ctx context.Context, id string) error {
	i.lock.Lock()
	defer i.lock.Unlock()
	_, ok := i.clients[id]
	if !ok {
		return ErrNotFound
	}
	delete(i.clients, id)
	return nil
}

func (i *InMemoryApi) ReadClient(ctx context.Context, id string) (*authclient.Client, error) {
	i.lock.Lock()
	defer i.lock.Unlock()
	v, ok := i.clients[id]
	if !ok {
		return nil, ErrNotFound
	}
	return v, nil
}

func (i *InMemoryApi) ReadClientByMetadata(ctx context.Context, metadata map[string]string) (*authclient.Client, error) {
	i.lock.Lock()
	defer i.lock.Unlock()
l:
	for _, client := range i.clients {
		if client.Metadata == nil {
			continue
		}
		for k, v := range metadata {
			if (*client.Metadata)[k] != v {
				continue l
			}
		}
		return client, nil
	}
	return nil, ErrNotFound
}

func (i *InMemoryApi) AddScopeToClient(ctx context.Context, clientId, scopeId string) error {
	i.lock.Lock()
	defer i.lock.Unlock()
	v, ok := i.clients[clientId]
	if !ok {
		return ErrNotFound
	}
	v.Scopes = append(v.Scopes, scopeId)
	return nil
}

func (i *InMemoryApi) DeleteScopeFromClient(ctx context.Context, clientId, scopeId string) error {
	i.lock.Lock()
	defer i.lock.Unlock()
	v, ok := i.clients[clientId]
	if !ok {
		return ErrNotFound
	}
	v.Scopes = Filter(v.Scopes, NotEqual(scopeId))
	return nil
}

func (i *InMemoryApi) Reset() {
	i.lock.Lock()
	defer i.lock.Unlock()
	i.clients = map[string]*authclient.Client{}
	i.scopes = map[string]*authclient.Scope{}
}

func NewInMemoryAPI() *InMemoryApi {
	return &InMemoryApi{
		scopes:  map[string]*authclient.Scope{},
		clients: map[string]*authclient.Client{},
	}
}

var _ API = (*InMemoryApi)(nil)

type AuthServerReferencer interface {
	client.Object
	AuthServerReference() string
}

type APIFactory interface {
	Create(referencer AuthServerReferencer) API
}
type ApiFactoryFn func(referencer AuthServerReferencer) API

func (fn ApiFactoryFn) Create(referencer AuthServerReferencer) API {
	return fn(referencer)
}

var DefaultApiFactory = ApiFactoryFn(func(referencer AuthServerReferencer) API {
	configuration := authclient.NewConfiguration()
	configuration.Servers = []authclient.ServerConfiguration{{
		URL: fmt.Sprintf("http://%s-%s.%s.svc.cluster.local:8080",
			referencer.GetNamespace(),
			referencer.AuthServerReference(),
			referencer.GetNamespace()),
	}}
	return NewDefaultServerApi(authclient.NewAPIClient(configuration))
})

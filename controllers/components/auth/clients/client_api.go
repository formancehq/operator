package clients

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/numary/auth/authclient"
	. "github.com/numary/formance-operator/pkg/collectionutil"
)

type ClientAPI interface {
	CreateClient(ctx context.Context, options authclient.ClientOptions) (*authclient.Client, error)
	UpdateClient(ctx context.Context, id string, options authclient.ClientOptions) error
	DeleteClient(ctx context.Context, id string) error
	ReadClient(ctx context.Context, id string) (*authclient.Client, error)
	ReadClientByMetadata(ctx context.Context, metadata map[string]string) (*authclient.Client, error)
	AddScopeToClient(ctx context.Context, clientId, scopeId string) error
	DeleteScopeFromClient(ctx context.Context, clientId, scopeId string) error
}

type defaultClientApi struct {
	*authclient.APIClient
}

func (d defaultClientApi) CreateClient(ctx context.Context, options authclient.ClientOptions) (*authclient.Client, error) {
	ret, _, err := d.DefaultApi.CreateClient(ctx).Body(options).Execute()
	if err != nil {
		return nil, err
	}
	return ret.Data, nil
}

func (d defaultClientApi) UpdateClient(ctx context.Context, id string, options authclient.ClientOptions) error {
	_, httpResponse, err := d.DefaultApi.UpdateClient(ctx, id).Body(options).Execute()
	if err != nil {
		if httpResponse.StatusCode == http.StatusNotFound {
			return ErrNotFound
		}
		return err
	}
	return nil
}

func (d defaultClientApi) DeleteClient(ctx context.Context, id string) error {
	httpResponse, err := d.DefaultApi.DeleteClient(ctx, id).Execute()
	if err != nil {
		if httpResponse.StatusCode == http.StatusNotFound {
			return ErrNotFound
		}
		return err
	}
	return nil
}

func (d defaultClientApi) ReadClient(ctx context.Context, id string) (*authclient.Client, error) {
	ret, httpResponse, err := d.DefaultApi.ReadClient(ctx, id).Execute()
	if err != nil {
		if httpResponse.StatusCode == http.StatusNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return ret.Data, nil
}

func (d defaultClientApi) ReadClientByMetadata(ctx context.Context, metadata map[string]string) (*authclient.Client, error) {
	clients, _, err := d.DefaultApi.ListClients(ctx).Execute()
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

func (d defaultClientApi) AddScopeToClient(ctx context.Context, clientId, scopeId string) error {
	httpResponse, err := d.DefaultApi.AddScopeToClient(ctx, clientId, scopeId).Execute()
	if err != nil {
		if httpResponse.StatusCode == http.StatusNotFound {
			return ErrNotFound
		}
		return err
	}
	return nil
}

func (d defaultClientApi) DeleteScopeFromClient(ctx context.Context, clientId, scopeId string) error {
	httpResponse, err := d.DefaultApi.DeleteScopeFromClient(ctx, clientId, scopeId).Execute()
	if err != nil {
		if httpResponse.StatusCode == http.StatusNotFound {
			return ErrNotFound
		}
		return err
	}
	return nil
}

var _ ClientAPI = (*defaultClientApi)(nil)

func NewDefaultClientAPI(apiClient *authclient.APIClient) *defaultClientApi {
	return &defaultClientApi{
		APIClient: apiClient,
	}
}

type inMemoryClientApi struct {
	clients map[string]*authclient.Client
}

func (i *inMemoryClientApi) CreateClient(ctx context.Context, options authclient.ClientOptions) (*authclient.Client, error) {
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

func (i *inMemoryClientApi) UpdateClient(ctx context.Context, id string, options authclient.ClientOptions) error {
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

func (i *inMemoryClientApi) DeleteClient(ctx context.Context, id string) error {
	_, ok := i.clients[id]
	if !ok {
		return ErrNotFound
	}
	delete(i.clients, id)
	return nil
}

func (i *inMemoryClientApi) ReadClient(ctx context.Context, id string) (*authclient.Client, error) {
	v, ok := i.clients[id]
	if !ok {
		return nil, ErrNotFound
	}
	return v, nil
}

func (i *inMemoryClientApi) ReadClientByMetadata(ctx context.Context, metadata map[string]string) (*authclient.Client, error) {
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

func (i *inMemoryClientApi) AddScopeToClient(ctx context.Context, clientId, scopeId string) error {
	v, ok := i.clients[clientId]
	if !ok {
		return ErrNotFound
	}
	v.Scopes = append(v.Scopes, scopeId)
	return nil
}

func (i *inMemoryClientApi) DeleteScopeFromClient(ctx context.Context, clientId, scopeId string) error {
	v, ok := i.clients[clientId]
	if !ok {
		return ErrNotFound
	}
	v.Scopes = Filter(v.Scopes, NotEqual(scopeId))
	return nil
}

func (i *inMemoryClientApi) reset() {
	i.clients = map[string]*authclient.Client{}
}

var _ ClientAPI = (*inMemoryClientApi)(nil)

func newInMemoryClientAPI() *inMemoryClientApi {
	return &inMemoryClientApi{
		clients: map[string]*authclient.Client{},
	}
}

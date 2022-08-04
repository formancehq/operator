package scopes

import (
	"context"

	"github.com/google/uuid"
	"github.com/numary/auth/authclient"
	"github.com/numary/formance-operator/apis/auth.components/v1beta1"
	"github.com/numary/formance-operator/pkg/collectionutil"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
)

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

func newScope(transient ...string) *v1beta1.Scope {
	return v1beta1.NewScope(uuid.NewString(), uuid.NewString(), transient...)
}

func apiScopeLength() int {
	return len(api.scopes)
}

func getScope(scope *v1beta1.Scope) error {
	return nsClient.Get(ctx, types.NamespacedName{
		Name: scope.Name,
	}, scope)
}

func isScopeSynchronized(scope *v1beta1.Scope) (bool, error) {
	if err := getScope(scope); err != nil {
		return false, err
	}
	return scope.Status.Synchronized, nil
}

func EventuallyApiHaveScopeLength(len int) {
	Eventually(apiScopeLength).WithOffset(1).Should(Equal(len))
}

func EventuallyScopeSynchronized(scope *v1beta1.Scope) {
	Eventually(func() (bool, error) {
		ok, err := isScopeSynchronized(scope)
		if err != nil {
			return false, err
		}
		return ok, nil
	}).WithOffset(1).Should(BeTrue())
}

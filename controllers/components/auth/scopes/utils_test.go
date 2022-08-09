package scopes

import (
	"github.com/google/uuid"
	"github.com/numary/formance-operator/apis/auth.components/v1beta1"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
)

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

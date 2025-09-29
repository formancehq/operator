package auths

import (
	"github.com/formancehq/operator/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/internal/core"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func checkAuthClientsReconciliation(ctx Context, auth *v1beta1.Auth) ([]*v1beta1.AuthClient, error) {
	condition := v1beta1.NewCondition("AuthClientsReconciliation", auth.Generation).SetMessage("AuthClientsReady")
	defer func() {
		auth.GetConditions().AppendOrReplace(*condition, v1beta1.AndConditions(
			v1beta1.ConditionTypeMatch("AuthClientsReconciliation"),
		))
	}()
	authClients := make([]*v1beta1.AuthClient, 0)
	if err := GetAllStackDependencies(ctx, auth.Spec.Stack, &authClients); err != nil {
		return nil, err
	}

	for _, client := range authClients {
		if !client.Status.Ready {
			condition.SetMessage("OneOfAuthClientsNotReady")
			condition.SetStatus(v1.ConditionFalse)
		}
	}

	return authClients, nil
}

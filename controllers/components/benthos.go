package components

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"strings"

	componentsv1beta2 "github.com/numary/operator/apis/components/v1beta2"
	apisv1beta1 "github.com/numary/operator/pkg/apis/v1beta1"
	"github.com/numary/operator/pkg/controllerutils"
	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

//go:embed benthos
var benthosConfigDir embed.FS

func searchIngesterName(service apisv1beta1.Object) string {
	return service.GetName() + "-search-ingester"
}

func buildStream(service client.Object, scheme *runtime.Scheme) (map[string]any, error) {
	groupVersionKinds, _, err := scheme.ObjectKinds(service)
	if err != nil {
		panic(err)
	}
	groupVersionKind := groupVersionKinds[0]

	data, err := benthosConfigDir.ReadFile(fmt.Sprintf("benthos/search/streams/%s.yaml",
		strings.ToLower(groupVersionKind.Kind)))
	if err != nil {
		return nil, err
	}

	stream := map[string]any{}
	if err := yaml.Unmarshal(data, &stream); err != nil {
		return nil, err
	}

	input := stream["input"].(map[string]any)
	eventBusInput := input["event_bus"].(map[string]any)
	eventBusInput["topic"] = fmt.Sprint(service.GetName())
	eventBusInput["consumer_group"] = service.GetName()

	return stream, nil
}

func reconcileSearchIngester(ctx context.Context, client client.Client, scheme *runtime.Scheme, service apisv1beta1.Object) error {
	_, ret, err := controllerutils.CreateOrUpdateWithController(ctx, client, scheme, types.NamespacedName{
		Namespace: service.GetNamespace(),
		Name:      searchIngesterName(service),
	}, service, func(t *componentsv1beta2.SearchIngester) error {

		stream, err := buildStream(service, scheme)
		if err != nil {
			return err
		}

		data, err := json.Marshal(stream)
		if err != nil {
			return err
		}

		t.Spec.Stream = data
		t.Spec.Reference = fmt.Sprintf("%s-search", service.GetNamespace())
		return nil
	})
	switch {
	case err != nil:
		apisv1beta1.SetCondition(service, "IngestionStreamReady", metav1.ConditionFalse, err.Error())
		return err
	case ret == controllerutil.OperationResultNone:
	default:
		apisv1beta1.SetCondition(service, "IngestionStreamReady", metav1.ConditionTrue)
	}
	return nil
}

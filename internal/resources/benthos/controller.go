package benthos

import (
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/formancehq/go-libs/v5/pkg/types/collections"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/applications"
	"github.com/formancehq/operator/v3/internal/resources/registries"
	"github.com/formancehq/operator/v3/internal/resources/resourcereferences"
	"github.com/formancehq/operator/v3/internal/resources/services"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

//go:embed builtin-templates
var builtinTemplates embed.FS

//+kubebuilder:rbac:groups=formance.com,resources=benthos,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=formance.com,resources=benthos/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=formance.com,resources=benthos/finalizers,verbs=update

func Reconcile(ctx Context, stack *v1beta1.Stack, b *v1beta1.Benthos) error {

	if err := createDeployment(ctx, stack, b); err != nil {
		return err
	}

	if err := createService(ctx, b); err != nil {
		return err
	}

	return nil
}

func createService(ctx Context, b *v1beta1.Benthos) error {
	_, err := services.Create(ctx, b, "benthos", func(t *corev1.Service) error {
		t.Labels = map[string]string{
			"app.kubernetes.io/service-name": "benthos",
		}
		t.Spec = corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name:       "http",
				Port:       4195,
				Protocol:   "TCP",
				TargetPort: intstr.FromString("http"),
			}},
			Selector: map[string]string{
				"app.kubernetes.io/name": "benthos",
			},
		}

		return nil
	})
	return err
}

// reconcileEnvFromResourceReferences creates a ResourceReference for every
// secret/configmap referenced from .spec.envFrom so the operator copies them
// into the stack namespace and rolls the deployment when their content changes.
// Stale references from previous reconciles are deleted.
func reconcileEnvFromResourceReferences(ctx Context, b *v1beta1.Benthos) (map[string]string, error) {
	hashes := map[string]string{}
	expected := map[string]struct{}{}

	for i, envFrom := range b.Spec.EnvFrom {
		if envFrom.SecretRef != nil && envFrom.SecretRef.Name != "" {
			refName := fmt.Sprintf("benthos-envfrom-%d-secret", i)
			ref, err := resourcereferences.Create(ctx, b, refName, envFrom.SecretRef.Name, &corev1.Secret{})
			if err != nil {
				return nil, err
			}
			expected[refName] = struct{}{}
			hashes[refName] = ref.Status.Hash
		}
		if envFrom.ConfigMapRef != nil && envFrom.ConfigMapRef.Name != "" {
			refName := fmt.Sprintf("benthos-envfrom-%d-configmap", i)
			ref, err := resourcereferences.Create(ctx, b, refName, envFrom.ConfigMapRef.Name, &corev1.ConfigMap{})
			if err != nil {
				return nil, err
			}
			expected[refName] = struct{}{}
			hashes[refName] = ref.Status.Hash
		}
	}

	list := &v1beta1.ResourceReferenceList{}
	if err := ctx.GetClient().List(ctx, list); err != nil {
		return nil, err
	}
	prefix := fmt.Sprintf("%s-benthos-envfrom-", b.Name)
	for _, ref := range list.Items {
		if !strings.HasPrefix(ref.Name, prefix) {
			continue
		}
		ownedByBenthos := false
		for _, or := range ref.OwnerReferences {
			if or.UID == b.UID {
				ownedByBenthos = true
				break
			}
		}
		if !ownedByBenthos {
			continue
		}
		shortName := strings.TrimPrefix(ref.Name, b.Name+"-")
		if _, ok := expected[shortName]; ok {
			continue
		}
		if err := resourcereferences.Delete(ctx, b, shortName); err != nil {
			return nil, err
		}
	}

	return hashes, nil
}

// We need to this controller and keep it focused on benthos
// buildTemplates merges the user-provided templates with the builtin ones
// shipped with the operator.
func buildTemplates(b *v1beta1.Benthos) (map[string]string, error) {
	ret := b.Spec.Templates
	if ret == nil {
		ret = make(map[string]string)
	}

	files, err := builtinTemplates.ReadDir("builtin-templates")
	if err != nil {
		return nil, errors.Wrap(err, "reading builtin templates directory")
	}

	for _, file := range files {
		data, err := builtinTemplates.ReadFile("builtin-templates/" + file.Name())
		if err != nil {
			return nil, errors.Wrapf(err, "reading builtin template '%s'", file.Name())
		}

		ret[file.Name()] = string(data)
	}

	return ret, nil
}

func createDeployment(ctx Context, stack *v1beta1.Stack, b *v1beta1.Benthos) error {
	serviceAccountName, err := settings.GetAWSServiceAccount(ctx, stack.Name)
	if err != nil {
		return err
	}

	// Cleanup potential old resource reference (pre v3.0.0)
	// todo(next-minor): remove
	err = resourcereferences.Delete(ctx, b, "elasticsearch")
	if err != nil {
		return err
	}

	envFromHashes, err := reconcileEnvFromResourceReferences(ctx, b)
	if err != nil {
		return err
	}

	awsIAMEnabled := serviceAccountName != ""

	broker := &v1beta1.Broker{}
	if err := ctx.GetClient().Get(ctx, types.NamespacedName{
		Name: stack.Name,
	}, broker); err != nil {
		return err
	}
	if !broker.Status.Ready {
		return NewPendingError().WithMessage("broker not ready")
	}

	var topicPrefix string
	switch broker.Status.Mode {
	case v1beta1.ModeOneStreamByService:
		topicPrefix = b.Spec.Stack + "-"
	case v1beta1.ModeOneStreamByStack:
		topicPrefix = b.Spec.Stack + "."
	}

	env := []corev1.EnvVar{
		Env("TOPIC_PREFIX", topicPrefix),
		Env("STACK", b.Spec.Stack),
		Env("BROKER", broker.Status.URI.Scheme),
	}
	if awsIAMEnabled {
		env = append(env, Env("AWS_IAM_ENABLED", "true"))
	}

	if broker.Status.URI.Scheme == "kafka" {
		env = append(env, Env("KAFKA_ADDRESS", broker.Status.URI.Host))
		if settings.IsTrue(broker.Status.URI.Query().Get("tls")) {
			env = append(env, Env("KAFKA_TLS_ENABLED", "true"))
		}
		if settings.IsTrue(broker.Status.URI.Query().Get("saslEnabled")) {
			env = append(env,
				Env("KAFKA_SASL_USERNAME", broker.Status.URI.Query().Get("saslUsername")),
				Env("KAFKA_SASL_PASSWORD", broker.Status.URI.Query().Get("saslPassword")),
				Env("KAFKA_SASL_MECHANISM", broker.Status.URI.Query().Get("saslMechanism")),
			)
		}
	}
	if broker.Status.URI.Scheme == "nats" {
		env = append(env, Env("NATS_URL", broker.Status.URI.Host))
		if broker.Status.Mode == v1beta1.ModeOneStreamByStack {
			env = append(env, Env("NATS_BIND", "true"))
		}
	}

	cmd := []string{
		"/benthos",
		"-r", "/resources/*.yaml",
		"-t", "/templates/*.yaml",
	}

	cmd = append(cmd, "--log.level", "trace", "streams", "/streams/*.yaml")

	// Drop config map if exists (pre v3.0.0)
	kinds, _, err := ctx.GetScheme().ObjectKinds(&corev1.ConfigMap{})
	if err != nil {
		return err
	}

	object := &unstructured.Unstructured{}
	object.SetGroupVersionKind(kinds[0])
	object.SetNamespace(stack.Name)
	object.SetName("benthos-audit")
	if err := client.IgnoreNotFound(ctx.GetClient().Delete(ctx, object)); err != nil {
		return errors.Wrap(err, "deleting audit config map")
	}

	volumes := make([]corev1.Volume, 0)
	volumeMounts := make([]corev1.VolumeMount, 0)
	configMaps := make([]*corev1.ConfigMap, 0)

	templates, err := buildTemplates(b)
	if err != nil {
		return err
	}

	for _, object := range []struct {
		discr string
		files map[string]string
	}{
		{
			discr: "resources",
			files: b.Spec.Resources,
		},
		{
			discr: "templates",
			files: templates,
		},
	} {

		configMapName := fmt.Sprintf("benthos-%s", object.discr)
		configMap, _, err := CreateOrUpdate[*corev1.ConfigMap](ctx, types.NamespacedName{
			Namespace: b.Spec.Stack,
			Name:      configMapName,
		},
			func(t *corev1.ConfigMap) error {
				t.Data = object.files
				if t.Data == nil {
					t.Data = make(map[string]string)
				}

				return nil
			},
			WithController[*corev1.ConfigMap](ctx.GetScheme(), b),
		)
		if err != nil {
			return err
		}

		configMaps = append(configMaps, configMap)

		volumeName := object.discr
		volumes = append(volumes, corev1.Volume{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMapName,
					},
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      volumeName,
			ReadOnly:  true,
			MountPath: fmt.Sprintf("/%s", object.discr),
		})
	}

	streamList := &v1beta1.BenthosStreamList{}
	if err := ctx.GetClient().List(ctx, streamList, client.MatchingFields{
		"stack": b.Spec.Stack,
	}); err != nil {
		return err
	}

	streams := streamList.Items
	sort.Slice(streams, func(i, j int) bool {
		return streams[i].Name < streams[j].Name
	})

	benthosImage, err := registries.GetBenthosImage(ctx, stack, "v4.23.1-es")
	if err != nil {
		return err
	}

	digest := sha256.New()
	for _, configMap := range configMaps {
		if err := json.NewEncoder(digest).Encode(configMap.Data); err != nil {
			return errors.Wrap(err, "hashing config map data")
		}
	}
	for _, stream := range streams {
		digest.Write([]byte(stream.Status.ConfigMapHash))
	}
	envFromHashKeys := make([]string, 0, len(envFromHashes))
	for k := range envFromHashes {
		envFromHashKeys = append(envFromHashKeys, k)
	}
	sort.Strings(envFromHashKeys)
	for _, k := range envFromHashKeys {
		digest.Write([]byte(k))
		digest.Write([]byte(envFromHashes[k]))
	}
	configHash := base64.StdEncoding.EncodeToString(digest.Sum(nil))

	podAnnotations := map[string]string{
		"config-hash": configHash,
	}

	return applications.
		New(b, &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: "benthos",
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: podAnnotations,
					},
					Spec: corev1.PodSpec{
						ImagePullSecrets: append(benthosImage.PullSecrets, b.Spec.ImagePullSecrets...),
						InitContainers:   b.Spec.InitContainers,
						Containers: []corev1.Container{{
							Name:    "benthos",
							Image:   benthosImage.GetFullImageName(),
							Env:     env,
							EnvFrom: b.Spec.EnvFrom,
							Command: cmd,
							Ports: []corev1.ContainerPort{{
								Name:          "http",
								ContainerPort: 4195,
							}},
							VolumeMounts: append(volumeMounts, corev1.VolumeMount{
								Name:      "streams",
								ReadOnly:  true,
								MountPath: "/streams",
							}),
						}},
						Volumes: append(volumes, corev1.Volume{
							Name: "streams",
							VolumeSource: corev1.VolumeSource{
								Projected: &corev1.ProjectedVolumeSource{
									Sources: Map(streams, func(stream v1beta1.BenthosStream) corev1.VolumeProjection {
										return corev1.VolumeProjection{
											ConfigMap: &corev1.ConfigMapProjection{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: fmt.Sprintf("stream-%s", stream.Name),
												},
												Items: []corev1.KeyToPath{{
													Key:  "stream.yaml",
													Path: stream.Spec.Name + ".yaml",
												}},
											},
										}
									}),
								},
							},
						}),
						ServiceAccountName: serviceAccountName,
					},
				},
			},
		}).
		Install(ctx)
}

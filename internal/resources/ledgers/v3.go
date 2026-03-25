package ledgers

import (
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/gatewayhttpapis"
	"github.com/formancehq/operator/v3/internal/resources/registries"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

const (
	v3PortHTTP = int32(9000)
	v3PortGRPC = int32(8888)
	v3PortRaft = int32(7777)
)

func reconcileV3(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, version string) error {
	imageConfiguration, err := registries.GetFormanceImage(ctx, stack, "ledger", version)
	if err != nil {
		return err
	}

	if err := gatewayhttpapis.Create(ctx, ledger, gatewayhttpapis.WithHealthCheckEndpoint("health")); err != nil {
		return err
	}

	if err := createV3HeadlessService(ctx, stack, ledger); err != nil {
		return err
	}

	// The GatewayHTTPAPI reconciler creates a ClusterIP service "ledger" with port 8080→"http".
	// Since our container port named "http" is 9000, the service routes 8080→9000 automatically.

	if err := installV3StatefulSet(ctx, stack, ledger, imageConfiguration); err != nil {
		return err
	}

	return nil
}

func createV3HeadlessService(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger) error {
	headlessSvcName := "ledger-raft"

	_, _, err := core.CreateOrUpdate[*corev1.Service](ctx, types.NamespacedName{
		Name:      headlessSvcName,
		Namespace: stack.Name,
	},
		func(t *corev1.Service) error {
			t.Spec = corev1.ServiceSpec{
				ClusterIP:                "None",
				PublishNotReadyAddresses: true,
				Ports: []corev1.ServicePort{
					{
						Name:       "raft",
						Port:       v3PortRaft,
						Protocol:   "TCP",
						TargetPort: intstr.FromString("raft"),
					},
					{
						Name:       "grpc",
						Port:       v3PortGRPC,
						Protocol:   "TCP",
						TargetPort: intstr.FromString("grpc"),
					},
				},
				Selector: map[string]string{
					"app.kubernetes.io/name": "ledger",
				},
			}
			return nil
		},
		core.WithController[*corev1.Service](ctx.GetScheme(), ledger),
	)
	return err
}

func installV3StatefulSet(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, image *registries.ImageConfiguration) error {
	stackName := stack.Name

	replicas, err := settings.GetInt32OrDefault(ctx, stackName, 3, "module", "ledger", "v3", "replicas")
	if err != nil {
		return err
	}
	if replicas%2 == 0 {
		return fmt.Errorf("module.ledger.v3.replicas must be odd, got %d", replicas)
	}

	volumeClaims, err := buildV3VolumeClaimTemplates(ctx, stackName)
	if err != nil {
		return err
	}

	podTemplate, err := buildV3PodTemplate(ctx, stack, ledger, image)
	if err != nil {
		return err
	}

	headlessSvcName := "ledger-raft"
	stsName := "ledger"

	_, _, err = core.CreateOrUpdate[*appsv1.StatefulSet](ctx, types.NamespacedName{
		Name:      stsName,
		Namespace: stackName,
	},
		func(t *appsv1.StatefulSet) error {
			t.Spec = appsv1.StatefulSetSpec{
				Replicas:            &replicas,
				ServiceName:         headlessSvcName,
				PodManagementPolicy: appsv1.OrderedReadyPodManagement,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app.kubernetes.io/name": "ledger",
					},
				},
				Template:             *podTemplate,
				VolumeClaimTemplates: volumeClaims,
			}
			return nil
		},
		core.WithController[*appsv1.StatefulSet](ctx.GetScheme(), ledger),
	)
	return err
}

func buildV3PodTemplate(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, image *registries.ImageConfiguration) (*corev1.PodTemplateSpec, error) {
	stackName := stack.Name

	otlpEnv, err := settings.GetOTELEnvVars(ctx, stackName, core.LowerCamelCaseKind(ctx, ledger), " ")
	if err != nil {
		return nil, err
	}

	clusterID, err := settings.GetStringOrDefault(ctx, stackName, "default", "module", "ledger", "v3", "cluster-id")
	if err != nil {
		return nil, err
	}

	dataDir := "/data/app"
	walDir := "/data/raft"

	env := []corev1.EnvVar{
		core.Env("BIND_ADDR", fmt.Sprintf("0.0.0.0:%d", v3PortRaft)),
		core.Env("CLUSTER_ID", clusterID),
		core.Env("GRPC_PORT", fmt.Sprint(v3PortGRPC)),
		core.Env("HTTP_PORT", fmt.Sprint(v3PortHTTP)),
		core.Env("WAL_DIR", walDir),
		core.Env("DATA_DIR", dataDir),
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
			},
		},
		{
			Name: "POD_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
			},
		},
	}
	env = append(env, otlpEnv...)
	env = append(env, core.GetDevEnvVars(stack, ledger)...)

	// Add pebble settings
	pebbleEnv, err := buildV3PebbleEnvVars(ctx, stackName)
	if err != nil {
		return nil, err
	}
	env = append(env, pebbleEnv...)

	// Add raft settings
	raftEnv, err := buildV3RaftEnvVars(ctx, stackName)
	if err != nil {
		return nil, err
	}
	env = append(env, raftEnv...)

	headlessSvcName := "ledger-raft"
	command := buildV3Command(headlessSvcName, dataDir)

	container := corev1.Container{
		Name:    "ledger",
		Image:   image.GetFullImageName(),
		Command: []string{"/bin/sh", "-c"},
		Args:    []string{command},
		Env:     env,
		Ports: []corev1.ContainerPort{
			{Name: "http", ContainerPort: v3PortHTTP},
			{Name: "grpc", ContainerPort: v3PortGRPC},
			{Name: "raft", ContainerPort: v3PortRaft},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "wal", MountPath: walDir},
			{Name: "data", MountPath: dataDir},
			{Name: "cold-cache", MountPath: "/data/cold-cache"},
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/livez",
					Port: intstr.FromString("http"),
				},
			},
			FailureThreshold: 20,
		},
		StartupProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/livez",
					Port: intstr.FromString("http"),
				},
			},
			FailureThreshold: 30,
			PeriodSeconds:    10,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/readyz",
					Port: intstr.FromString("http"),
				},
			},
		},
		Lifecycle: &corev1.Lifecycle{
			PreStop: &corev1.LifecycleHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"/bin/sh", "-c", buildV3PreStopScript(walDir)},
				},
			},
		},
	}

	return &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"app.kubernetes.io/name": "ledger",
			},
		},
		Spec: corev1.PodSpec{
			ImagePullSecrets: image.PullSecrets,
			Containers:       []corev1.Container{container},
		},
	}, nil
}

func buildV3Command(headlessSvc, dataDir string) string {
	// Shell script that computes node-id from the StatefulSet ordinal index,
	// builds the advertise-addr from the pod's DNS name within the headless service,
	// and decides whether to --bootstrap or --join depending on ordinal.
	//
	// POD_NAME is like "ledger-0", POD_INDEX is extracted from the suffix.
	// pod-0 bootstraps (if no existing state), other pods join pod-0.
	lines := []string{
		// Extract the ordinal index from the pod name (e.g. "ledger-2" → "2")
		`POD_INDEX=${POD_NAME##*-}`,
		// Raft node IDs must be >= 1
		`NODE_ID=$((POD_INDEX + 1))`,
		// FQDN within the headless service
		fmt.Sprintf(`ADVERTISE_ADDR="${POD_NAME}.%s.${POD_NAMESPACE}.svc.cluster.local:%d"`, headlessSvc, v3PortRaft),
		// First pod (ordinal 0) bootstraps if no checkpoint exists yet, otherwise normal start.
		// Other pods join pod-0.
		fmt.Sprintf(`BOOTSTRAP_ADDR="ledger-0.%s.${POD_NAMESPACE}.svc.cluster.local:%d"`, headlessSvc, v3PortGRPC),
		`CLUSTER_FLAG=""`,
		fmt.Sprintf(`if [ "$POD_INDEX" = "0" ]; then
  if [ ! -d "%s/pebble" ]; then
    CLUSTER_FLAG="--bootstrap"
  fi
else
  CLUSTER_FLAG="--join $BOOTSTRAP_ADDR"
fi`, dataDir),
		// Exec into the ledger binary
		`exec ./ledger run \`,
		`  --node-id "$NODE_ID" \`,
		`  --advertise-addr "$ADVERTISE_ADDR" \`,
		`  $CLUSTER_FLAG`,
	}

	return strings.Join(lines, "\n")
}

// buildV3PreStopScript returns a shell script executed by the Kubernetes preStop
// lifecycle hook before a pod is terminated. It deregisters the local node from
// the Raft cluster and cleans the WAL directory so that a future re-join (after
// scale-up) starts as a fresh learner.
func buildV3PreStopScript(walDir string) string {
	lines := []string{
		// Best-effort deregister: call the admin endpoint to remove this node
		// from the Raft cluster. Ignore errors (e.g., last node, or already removed).
		fmt.Sprintf(`wget --post-data='' -q -O- http://localhost:%d/_admin/deregister || true`, v3PortHTTP),
		// Clean WAL so that if this pod restarts (scale-up), it joins as a fresh learner.
		fmt.Sprintf(`rm -rf %s/* || true`, walDir),
	}

	return strings.Join(lines, "\n")
}

func buildV3VolumeClaimTemplates(ctx core.Context, stackName string) ([]corev1.PersistentVolumeClaim, error) {
	type volumeSpec struct {
		name            string
		sizeKey         string
		defaultSize     string
		storageClassKey string
	}

	specs := []volumeSpec{
		{"wal", "module.ledger.v3.persistence.wal.size", "5Gi", "module.ledger.v3.persistence.wal.storage-class"},
		{"data", "module.ledger.v3.persistence.data.size", "10Gi", "module.ledger.v3.persistence.data.storage-class"},
		{"cold-cache", "module.ledger.v3.persistence.cold-cache.size", "10Gi", "module.ledger.v3.persistence.cold-cache.storage-class"},
	}

	var claims []corev1.PersistentVolumeClaim
	for _, s := range specs {
		sizeStr, err := settings.GetStringOrDefault(ctx, stackName, s.defaultSize, strings.Split(s.sizeKey, ".")...)
		if err != nil {
			return nil, err
		}

		storageClass, err := settings.GetStringOrEmpty(ctx, stackName, strings.Split(s.storageClassKey, ".")...)
		if err != nil {
			return nil, err
		}

		pvc := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: s.name,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse(sizeStr),
					},
				},
			},
		}
		if storageClass != "" {
			pvc.Spec.StorageClassName = &storageClass
		}
		claims = append(claims, pvc)
	}

	return claims, nil
}

// pebble setting key → env var name
var v3PebbleSettings = []struct {
	key    string
	envVar string
}{
	{"cache-size", "PEBBLE_CACHE_SIZE"},
	{"memtable-size", "PEBBLE_MEMTABLE_SIZE"},
	{"memtable-stop-writes-threshold", "PEBBLE_MEMTABLE_STOP_WRITES_THRESHOLD"},
	{"l0-compaction-threshold", "PEBBLE_L0_COMPACTION_THRESHOLD"},
	{"l0-stop-writes-threshold", "PEBBLE_L0_STOP_WRITES_THRESHOLD"},
	{"lbase-max-bytes", "PEBBLE_LBASE_MAX_BYTES"},
	{"target-file-size", "PEBBLE_TARGET_FILE_SIZE"},
	{"max-concurrent-compactions", "PEBBLE_MAX_CONCURRENT_COMPACTIONS"},
}

func buildV3PebbleEnvVars(ctx core.Context, stackName string) ([]corev1.EnvVar, error) {
	var envVars []corev1.EnvVar
	for _, s := range v3PebbleSettings {
		val, err := settings.GetStringOrEmpty(ctx, stackName, "module", "ledger", "v3", "pebble", s.key)
		if err != nil {
			return nil, err
		}
		if val != "" {
			envVars = append(envVars, core.Env(s.envVar, val))
		}
	}
	return envVars, nil
}

var v3RaftSettings = []struct {
	key    string
	envVar string
}{
	{"snapshot-threshold", "RAFT_SNAPSHOT_THRESHOLD"},
	{"election-tick", "RAFT_ELECTION_TICK"},
	{"heartbeat-tick", "RAFT_HEARTBEAT_TICK"},
	{"tick-interval", "RAFT_TICK_INTERVAL"},
	{"max-size-per-msg", "RAFT_MAX_SIZE_PER_MSG"},
	{"max-inflight-msgs", "RAFT_MAX_INFLIGHT_MSGS"},
	{"compaction-margin", "RAFT_COMPACTION_MARGIN"},
}

func buildV3RaftEnvVars(ctx core.Context, stackName string) ([]corev1.EnvVar, error) {
	var envVars []corev1.EnvVar
	for _, s := range v3RaftSettings {
		val, err := settings.GetStringOrEmpty(ctx, stackName, "module", "ledger", "v3", "raft", s.key)
		if err != nil {
			return nil, err
		}
		if val != "" {
			envVars = append(envVars, core.Env(s.envVar, val))
		}
	}
	return envVars, nil
}

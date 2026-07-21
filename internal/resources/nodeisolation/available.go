package nodeisolation

import (
	"sync/atomic"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	"github.com/formancehq/operator/v3/internal/core"
)

// CRD names Karpenter installs for the two kinds this feature manages.
const (
	ec2NodeClassCRDName = "ec2nodeclasses.karpenter.k8s.aws"
	nodePoolCRDName     = "nodepools.karpenter.sh"
)

// available reports whether both Karpenter CRDs are installed. It is read on every
// reconcile and refreshed by DetectCRDs at controller setup and whenever a Karpenter CRD
// appears/disappears (see the CustomResourceDefinition watch in the stacks reconciler), so
// installing Karpenter after the operator starts does not require a restart.
var available atomic.Bool

// IsAvailable reports whether the Karpenter CRDs are currently installed.
func IsAvailable() bool { return available.Load() }

// SetAvailable overrides the availability flag. Intended for tests.
func SetAvailable(v bool) { available.Store(v) }

// IsKarpenterCRD reports whether the named CustomResourceDefinition is one of the
// Karpenter CRDs this feature depends on.
func IsKarpenterCRD(name string) bool {
	return name == ec2NodeClassCRDName || name == nodePoolCRDName
}

// DetectCRDs performs precise discovery (a targeted Get per CRD) and updates the
// availability flag. It sets available=true only if both Karpenter CRDs are present.
func DetectCRDs(ctx core.Context) error {
	for _, name := range []string{ec2NodeClassCRDName, nodePoolCRDName} {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := ctx.GetAPIReader().Get(ctx, types.NamespacedName{Name: name}, crd); err != nil {
			if apierrors.IsNotFound(err) {
				available.Store(false)
				return nil
			}
			return err
		}
	}
	available.Store(true)
	return nil
}

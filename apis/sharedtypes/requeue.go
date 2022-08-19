package sharedtypes

import (
	"sigs.k8s.io/controller-runtime"
)

func Requeue() *controllerruntime.Result {
	return &controllerruntime.Result{Requeue: true}
}

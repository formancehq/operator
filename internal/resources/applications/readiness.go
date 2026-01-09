package applications

import (
	corev1 "k8s.io/api/core/v1"
)

func DefaultReadiness(port string, opts ...ProbeOpts) *corev1.Probe {
	return readiness(newProbeHandler(port, opts...))
}
func readiness(handler corev1.ProbeHandler) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler:                  handler,
		InitialDelaySeconds:           1,
		TimeoutSeconds:                30,
		PeriodSeconds:                 2,
		SuccessThreshold:              1,
		FailureThreshold:              20,
		TerminationGracePeriodSeconds: nil,
	}
}

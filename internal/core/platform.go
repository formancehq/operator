package core

type Platform struct {
	// Cloud region where the stack is deployed
	Region string
	// Cloud environment where the stack is deployed: staging, production,
	// sandbox, etc.
	Environment string
	// The licence information
	LicenceSecret string
	// The operator utils image version
	UtilsVersion string
	// Namespace where the licence secret lives
	LicenceNamespace string
	// Licence validation state (computed from the licence secret JWT)
	LicenceState LicenceState
	// Human-readable message about the licence state
	LicenceMessage string
}

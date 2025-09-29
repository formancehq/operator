package manifests

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"golang.org/x/mod/semver"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Loader handles loading and caching of version manifests
type Loader struct {
	cache  sync.Map // map[string]*v1beta1.VersionManifest
	client client.Client
}

var globalLoader *Loader

// InitLoader initializes the global loader with a Kubernetes client
func InitLoader(c client.Client) {
	globalLoader = &Loader{
		client: c,
	}
}

// Load retrieves the manifest for a given component and version
func Load(ctx context.Context, component, version string) (*v1beta1.VersionManifest, error) {
	if globalLoader == nil {
		return nil, fmt.Errorf("loader not initialized, call InitLoader first")
	}
	return globalLoader.Load(ctx, component, version)
}

func (l *Loader) Load(ctx context.Context, component, version string) (*v1beta1.VersionManifest, error) {
	// Check cache
	cacheKey := fmt.Sprintf("%s@%s", component, version)
	if cached, ok := l.cache.Load(cacheKey); ok {
		return cached.(*v1beta1.VersionManifest), nil
	}

	// Load all manifests for this component from Kubernetes
	manifests, err := l.loadComponentManifests(ctx, component)
	if err != nil {
		return nil, fmt.Errorf("loading manifests for %s: %w", component, err)
	}

	// Find matching manifest
	matched := l.findMatchingManifest(manifests, version)
	if matched == nil {
		return nil, fmt.Errorf("no manifest found for %s version %s", component, version)
	}

	// Resolve inheritance
	resolved, err := l.resolveInheritance(ctx, component, matched, manifests)
	if err != nil {
		return nil, fmt.Errorf("resolving inheritance: %w", err)
	}

	// Cache and return
	l.cache.Store(cacheKey, resolved)
	return resolved, nil
}

func (l *Loader) loadComponentManifests(ctx context.Context, component string) ([]*v1beta1.VersionManifest, error) {
	// List all VersionManifests for this component
	list := &v1beta1.VersionManifestList{}
	if err := l.client.List(ctx, list, client.MatchingLabels{
		"formance.com/component": component,
	}); err != nil {
		return nil, fmt.Errorf("listing manifests: %w", err)
	}

	if len(list.Items) == 0 {
		return nil, fmt.Errorf("no manifests found for component %s", component)
	}

	manifests := make([]*v1beta1.VersionManifest, len(list.Items))
	for i := range list.Items {
		manifests[i] = &list.Items[i]
	}

	return manifests, nil
}

func (l *Loader) findMatchingManifest(manifests []*v1beta1.VersionManifest, version string) *v1beta1.VersionManifest {
	for _, m := range manifests {
		if l.versionMatches(version, m.Spec.VersionRange) {
			return m
		}
	}
	return nil
}

// versionMatches checks if a version satisfies a version range
// Supports: ">=v2.0.0", ">=v2.0.0 <v3.0.0", exact version
func (l *Loader) versionMatches(version, versionRange string) bool {
	// Handle "latest" or invalid semver
	if !semver.IsValid(version) {
		return versionRange == "latest" || versionRange == "" || strings.Contains(versionRange, ">=v")
	}

	// Parse range (simplified implementation)
	parts := strings.Fields(versionRange)
	if len(parts) == 0 {
		return false
	}

	// Simple case: exact match
	if len(parts) == 1 && !strings.HasPrefix(parts[0], ">") && !strings.HasPrefix(parts[0], "<") {
		return semver.Compare(version, parts[0]) == 0
	}

	// Parse operators
	for i := 0; i < len(parts); i++ {
		part := parts[i]

		if strings.HasPrefix(part, ">=") {
			operand := strings.TrimPrefix(part, ">=")
			if semver.Compare(version, operand) < 0 {
				return false
			}
		} else if strings.HasPrefix(part, ">") {
			operand := strings.TrimPrefix(part, ">")
			if semver.Compare(version, operand) <= 0 {
				return false
			}
		} else if strings.HasPrefix(part, "<=") {
			operand := strings.TrimPrefix(part, "<=")
			if semver.Compare(version, operand) > 0 {
				return false
			}
		} else if strings.HasPrefix(part, "<") {
			operand := strings.TrimPrefix(part, "<")
			if semver.Compare(version, operand) >= 0 {
				return false
			}
		}
	}

	return true
}

func (l *Loader) resolveInheritance(ctx context.Context, component string, manifest *v1beta1.VersionManifest, available []*v1beta1.VersionManifest) (*v1beta1.VersionManifest, error) {
	if manifest.Spec.Extends == "" {
		return manifest, nil
	}

	// Find parent manifest
	var parent *v1beta1.VersionManifest
	for _, m := range available {
		if m.Spec.VersionRange == manifest.Spec.Extends {
			parent = m
			break
		}
	}

	if parent == nil {
		return nil, fmt.Errorf("parent manifest not found: %s", manifest.Spec.Extends)
	}

	// Recursively resolve parent
	resolvedParent, err := l.resolveInheritance(ctx, component, parent, available)
	if err != nil {
		return nil, err
	}

	// Merge (child overrides parent)
	merged := l.mergeManifests(resolvedParent, manifest)
	return merged, nil
}

func (l *Loader) mergeManifests(parent, child *v1beta1.VersionManifest) *v1beta1.VersionManifest {
	result := &v1beta1.VersionManifest{
		TypeMeta:   child.TypeMeta,
		ObjectMeta: child.ObjectMeta,
		Spec:       child.Spec,
	}

	// Merge fields where child is empty/default
	// Note: EnvVarPrefix is NOT inherited - child can explicitly set it to "" to remove parent's prefix

	if result.Spec.Streams.Ingestion == "" {
		result.Spec.Streams.Ingestion = parent.Spec.Streams.Ingestion
	}

	if result.Spec.Streams.Reindex == "" {
		result.Spec.Streams.Reindex = parent.Spec.Streams.Reindex
	}

	// Merge features (child overrides)
	if result.Spec.Features == nil {
		result.Spec.Features = make(map[string]bool)
	}
	for k, v := range parent.Spec.Features {
		if _, exists := result.Spec.Features[k]; !exists {
			result.Spec.Features[k] = v
		}
	}

	// Gateway config
	if !result.Spec.Gateway.Enabled && parent.Spec.Gateway.Enabled {
		result.Spec.Gateway.Enabled = parent.Spec.Gateway.Enabled
	}
	if result.Spec.Gateway.HealthCheckEndpoint == "" {
		result.Spec.Gateway.HealthCheckEndpoint = parent.Spec.Gateway.HealthCheckEndpoint
	}

	// Migration config
	if !result.Spec.Migration.Enabled && parent.Spec.Migration.Enabled {
		result.Spec.Migration.Enabled = parent.Spec.Migration.Enabled
	}
	if result.Spec.Migration.Strategy == "" {
		result.Spec.Migration.Strategy = parent.Spec.Migration.Strategy
	}

	// Authorization scopes
	result.Spec.Authorization.Scopes = mergeScopes(
		parent.Spec.Authorization.Scopes,
		child.Spec.Authorization.Scopes,
	)

	return result
}

// mergeScopes merges parent and child scopes
// Child scopes override parent scopes with the same name
func mergeScopes(parent, child []v1beta1.ScopeDefinition) []v1beta1.ScopeDefinition {
	// Build map of child scopes by name for quick lookup
	childScopeMap := make(map[string]v1beta1.ScopeDefinition)
	for _, scope := range child {
		childScopeMap[scope.Name] = scope
	}

	// Start with all child scopes
	result := make([]v1beta1.ScopeDefinition, 0, len(parent)+len(child))
	result = append(result, child...)

	// Add parent scopes that are not overridden by child
	for _, parentScope := range parent {
		if _, exists := childScopeMap[parentScope.Name]; !exists {
			result = append(result, parentScope)
		}
	}

	return result
}

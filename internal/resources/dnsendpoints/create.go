package dnsendpoints

import (
	"fmt"
	"maps"
	"strings"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/external-dns/apis/v1alpha1"
	"sigs.k8s.io/external-dns/endpoint"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/settings"
)

type DNSConfig struct {
	Enabled      bool
	DNSPatterns  []string
	Targets      []string
	ProviderSpec map[string]string
	Annotations  map[string]string
	RecordType   string
}

func getDNSConfig(ctx core.Context, stack string, dnsType string) (*DNSConfig, error) {
	enabled, err := settings.GetBoolOrDefault(ctx, stack, false, "dns", dnsType, "enabled")
	if err != nil {
		return nil, err
	}

	if !enabled {
		return nil, nil
	}

	dnsNameStr, err := settings.GetStringOrEmpty(ctx, stack, "dns", dnsType, "dns-name")
	if err != nil {
		return nil, err
	}
	if dnsNameStr == "" {
		return nil, fmt.Errorf("dns.%s.dns-name is required when dns.%s.enabled is true", dnsType, dnsType)
	}
	dnsNames := strings.Split(dnsNameStr, ",")
	for i := range dnsNames {
		dnsNames[i] = strings.TrimSpace(dnsNames[i])
	}

	targetsStr, err := settings.GetStringOrEmpty(ctx, stack, "dns", dnsType, "targets")
	if err != nil {
		return nil, err
	}
	if targetsStr == "" {
		return nil, fmt.Errorf("dns.%s.targets is required when dns.%s.enabled is true", dnsType, dnsType)
	}
	targets := strings.Split(targetsStr, ",")
	for i := range targets {
		targets[i] = strings.TrimSpace(targets[i])
	}

	providerSpec, err := settings.GetMapOrEmpty(ctx, stack, "dns", dnsType, "provider-specific")
	if err != nil {
		return nil, err
	}

	annotations, err := settings.GetMapOrEmpty(ctx, stack, "dns", dnsType, "annotations")
	if err != nil {
		return nil, err
	}

	recordType, err := settings.GetStringOrDefault(ctx, stack, "CNAME", "dns", dnsType, "record-type")
	if err != nil {
		return nil, err
	}

	return &DNSConfig{
		Enabled:      enabled,
		DNSPatterns:  dnsNames,
		Targets:      targets,
		ProviderSpec: providerSpec,
		Annotations:  annotations,
		RecordType:   recordType,
	}, nil
}

func expandDNSPattern(pattern, stack string) string {
	return strings.ReplaceAll(pattern, "{stack}", stack)
}

func createDNSEndpoint(ctx core.Context, stack *v1beta1.Stack, dnsType string, config *DNSConfig) error {
	name := fmt.Sprintf("%s-%s", stack.Name, dnsType)

	// Build endpoints array - one endpoint per DNS name
	endpoints := []*endpoint.Endpoint{}
	for _, dnsPattern := range config.DNSPatterns {
		dnsName := expandDNSPattern(dnsPattern, stack.Name)
		ep := &endpoint.Endpoint{
			DNSName:    dnsName,
			RecordType: config.RecordType,
			Targets:    endpoint.Targets(config.Targets),
		}

		// Add provider-specific settings if present
		if len(config.ProviderSpec) > 0 {
			providerSpecific := endpoint.ProviderSpecific{}
			for name, value := range config.ProviderSpec {
				providerSpecific = append(providerSpecific, endpoint.ProviderSpecificProperty{
					Name:  name,
					Value: value,
				})
			}
			ep.ProviderSpecific = providerSpecific
		}

		endpoints = append(endpoints, ep)
	}

	// Create or update the DNSEndpoint
	_, _, err := core.CreateOrUpdate(ctx, types.NamespacedName{
		Name:      name,
		Namespace: stack.Name,
	}, func(d *v1alpha1.DNSEndpoint) error {
		// Set the spec
		d.Spec.Endpoints = endpoints

		// Set annotations
		if len(config.Annotations) > 0 {
			annotations := d.GetAnnotations()
			if annotations == nil {
				annotations = make(map[string]string)
			}
			maps.Copy(annotations, config.Annotations)
			d.SetAnnotations(annotations)
		}

		// Set owner reference
		return core.WithController[*v1alpha1.DNSEndpoint](ctx.GetScheme(), stack)(d)
	})

	return err
}

func Reconcile(ctx core.Context, stack *v1beta1.Stack) error {
	// Handle private DNS endpoint
	privateConfig, err := getDNSConfig(ctx, stack.Name, "private")
	if err != nil {
		return err
	}
	if privateConfig != nil {
		if err := createDNSEndpoint(ctx, stack, "private", privateConfig); err != nil {
			return err
		}
	} else {
		// Delete private DNS endpoint if it exists and is disabled
		if err := deleteDNSEndpoint(ctx, stack.Name, fmt.Sprintf("%s-private", stack.Name)); err != nil {
			return err
		}
	}

	// Handle public DNS endpoint
	publicConfig, err := getDNSConfig(ctx, stack.Name, "public")
	if err != nil {
		return err
	}
	if publicConfig != nil {
		if err := createDNSEndpoint(ctx, stack, "public", publicConfig); err != nil {
			return err
		}
	} else {
		// Delete public DNS endpoint if it exists and is disabled
		if err := deleteDNSEndpoint(ctx, stack.Name, fmt.Sprintf("%s-public", stack.Name)); err != nil {
			return err
		}
	}

	return nil
}

func deleteDNSEndpoint(ctx core.Context, namespace, name string) error {
	dnsEndpoint := &v1alpha1.DNSEndpoint{}
	dnsEndpoint.SetName(name)
	dnsEndpoint.SetNamespace(namespace)

	if err := ctx.GetClient().Get(ctx, types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}, dnsEndpoint); err != nil {
		// Resource doesn't exist, nothing to delete
		return client.IgnoreNotFound(err)
	}

	return ctx.GetClient().Delete(ctx, dnsEndpoint)
}

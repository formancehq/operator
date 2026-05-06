package otelexporterendpoints

import (
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
)

type collectorConfig struct {
	Receivers  collectorReceivers `yaml:"receivers"`
	Processors map[string]any     `yaml:"processors,omitempty"`
	Exporters  map[string]any     `yaml:"exporters"`
	Service    collectorService   `yaml:"service"`
}

type collectorReceivers struct {
	OTLP otlpReceiver `yaml:"otlp"`
}

type otlpReceiver struct {
	Protocols otlpProtocols `yaml:"protocols"`
}

type otlpProtocols struct {
	HTTP otlpHTTP `yaml:"http"`
}

type otlpHTTP struct {
	Endpoint string `yaml:"endpoint"`
}

type collectorService struct {
	Pipelines map[string]collectorPipeline `yaml:"pipelines"`
}

type collectorPipeline struct {
	Receivers  []string `yaml:"receivers"`
	Processors []string `yaml:"processors,omitempty"`
	Exporters  []string `yaml:"exporters"`
}

type otlpExporter struct {
	Endpoint string            `yaml:"endpoint"`
	Headers  map[string]string `yaml:"headers,omitempty"`
}

type resourceProcessorAttribute struct {
	Key    string `yaml:"key"`
	Value  string `yaml:"value"`
	Action string `yaml:"action"`
}

type resourceProcessor struct {
	Attributes []resourceProcessorAttribute `yaml:"attributes"`
}

type exporterInput struct {
	name     string
	signal   *v1beta1.OtelSignalConfig
	envAlias string
}

func inferProtocol(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err == nil && u.Scheme == "grpc" {
		return "grpc"
	}
	return "http"
}

func stripScheme(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	if u.Scheme == "grpc" {
		return u.Host
	}
	return endpoint
}

func buildExporter(input exporterInput) (string, any) {
	protocol := inferProtocol(input.signal.Endpoint)
	endpoint := stripScheme(input.signal.Endpoint)

	var headers map[string]string
	if input.signal.Auth != nil && input.signal.Auth.Type == "bearer" {
		headers = map[string]string{
			"authorization": fmt.Sprintf("Bearer ${env:%s}", input.envAlias),
		}
	}

	prefix := "otlphttp/"
	if protocol == "grpc" {
		prefix = "otlp/"
	}
	return prefix + input.name, otlpExporter{
		Endpoint: endpoint,
		Headers:  headers,
	}
}

type collectorInput struct {
	Endpoint        *v1beta1.OtelExporterEndpoint
	TracesEnvAlias  string
	MetricsEnvAlias string
}

type otelSettingsInput struct {
	TracesEndpoint  string
	MetricsEndpoint string
}

func generateMergedCollectorConfig(endpoints []collectorInput, otelSettings *otelSettingsInput) (string, error) {
	exporters := map[string]any{}
	processors := map[string]any{}
	var tracesPipelines []pipelineContribution
	var metricsPipelines []pipelineContribution

	sortedEndpoints := make([]collectorInput, len(endpoints))
	copy(sortedEndpoints, endpoints)
	sort.Slice(sortedEndpoints, func(i, j int) bool {
		return sortedEndpoints[i].Endpoint.Name < sortedEndpoints[j].Endpoint.Name
	})

	for _, ci := range sortedEndpoints {
		ep := ci.Endpoint
		crdName := sanitizeName(ep.Name)

		var resourceProc string
		if len(ep.Spec.ResourceAttributes) > 0 {
			procName := "resource/" + crdName
			attrs := make([]resourceProcessorAttribute, 0, len(ep.Spec.ResourceAttributes))
			keys := make([]string, 0, len(ep.Spec.ResourceAttributes))
			for k := range ep.Spec.ResourceAttributes {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			for _, k := range keys {
				attrs = append(attrs, resourceProcessorAttribute{
					Key:    k,
					Value:  ep.Spec.ResourceAttributes[k],
					Action: "upsert",
				})
			}
			processors[procName] = resourceProcessor{Attributes: attrs}
			resourceProc = procName
		}

		if ep.Spec.Traces != nil && ep.Spec.Traces.Endpoint != "" {
			name, exp := buildExporter(exporterInput{
				name:     crdName + "-traces",
				signal:   ep.Spec.Traces,
				envAlias: ci.TracesEnvAlias,
			})
			exporters[name] = exp
			tracesPipelines = append(tracesPipelines, pipelineContribution{
				exporter:  name,
				processor: resourceProc,
			})
		}

		if ep.Spec.Metrics != nil && ep.Spec.Metrics.Endpoint != "" {
			name, exp := buildExporter(exporterInput{
				name:     crdName + "-metrics",
				signal:   ep.Spec.Metrics,
				envAlias: ci.MetricsEnvAlias,
			})
			exporters[name] = exp
			metricsPipelines = append(metricsPipelines, pipelineContribution{
				exporter:  name,
				processor: resourceProc,
			})
		}
	}

	if otelSettings != nil {
		if otelSettings.TracesEndpoint != "" {
			name, exp := buildExporter(exporterInput{
				name:   "settings-traces",
				signal: &v1beta1.OtelSignalConfig{Endpoint: otelSettings.TracesEndpoint},
			})
			exporters[name] = exp
			tracesPipelines = append(tracesPipelines, pipelineContribution{exporter: name})
		}
		if otelSettings.MetricsEndpoint != "" {
			name, exp := buildExporter(exporterInput{
				name:   "settings-metrics",
				signal: &v1beta1.OtelSignalConfig{Endpoint: otelSettings.MetricsEndpoint},
			})
			exporters[name] = exp
			metricsPipelines = append(metricsPipelines, pipelineContribution{exporter: name})
		}
	}

	if len(tracesPipelines) == 0 && len(metricsPipelines) == 0 {
		exporters["nop"] = struct{}{}
		tracesPipelines = []pipelineContribution{{exporter: "nop"}}
		metricsPipelines = []pipelineContribution{{exporter: "nop"}}
	}

	if len(tracesPipelines) == 0 {
		exporters["nop"] = struct{}{}
		tracesPipelines = []pipelineContribution{{exporter: "nop"}}
	}
	if len(metricsPipelines) == 0 {
		exporters["nop"] = struct{}{}
		metricsPipelines = []pipelineContribution{{exporter: "nop"}}
	}

	pipelines := buildPipelines(tracesPipelines, metricsPipelines)

	cfg := collectorConfig{
		Receivers: collectorReceivers{
			OTLP: otlpReceiver{
				Protocols: otlpProtocols{
					HTTP: otlpHTTP{
						Endpoint: "0.0.0.0:4318",
					},
				},
			},
		},
		Exporters:  exporters,
		Processors: processors,
		Service: collectorService{
			Pipelines: pipelines,
		},
	}

	if len(processors) == 0 {
		cfg.Processors = nil
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

type pipelineContribution struct {
	exporter  string
	processor string
}

func buildPipelines(traces, metrics []pipelineContribution) map[string]collectorPipeline {
	pipelines := map[string]collectorPipeline{}
	addSignalPipelines(pipelines, "traces", traces)
	addSignalPipelines(pipelines, "metrics", metrics)
	return pipelines
}

func addSignalPipelines(pipelines map[string]collectorPipeline, signal string, contributions []pipelineContribution) {
	grouped := groupByProcessor(contributions)
	if len(grouped) == 1 {
		for proc, exporters := range grouped {
			p := collectorPipeline{
				Receivers: []string{"otlp"},
				Exporters: exporters,
			}
			if proc != "" {
				p.Processors = []string{proc}
			}
			pipelines[signal] = p
		}
		return
	}
	for proc, exporterList := range grouped {
		suffix := "default"
		if proc != "" {
			parts := strings.SplitN(proc, "/", 2)
			if len(parts) == 2 {
				suffix = parts[1]
			}
		}
		p := collectorPipeline{
			Receivers: []string{"otlp"},
			Exporters: exporterList,
		}
		if proc != "" {
			p.Processors = []string{proc}
		}
		pipelines[signal+"/"+suffix] = p
	}
}

func groupByProcessor(contributions []pipelineContribution) map[string][]string {
	grouped := map[string][]string{}
	for _, c := range contributions {
		grouped[c.processor] = append(grouped[c.processor], c.exporter)
	}
	return grouped
}

func sanitizeName(name string) string {
	return strings.ReplaceAll(name, ".", "-")
}

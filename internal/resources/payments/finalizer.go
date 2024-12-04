package payments

import (
	"crypto/tls"
	"fmt"
	"sync"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/settings"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func Clean(ctx core.Context, t *v1beta1.Payments) error {
	temporalNamespace, client, err := getTemporalClient(ctx, t.Spec.Stack)
	if err != nil {
		return err
	}
	defer client.Close()

	if client == nil {
		return nil
	}

	if err := cleanTemporalSchedules(ctx, client, t.Spec.Stack); err != nil {
		return err
	}

	if err := cleanTemporalWorkflows(ctx, client, temporalNamespace, t.Spec.Stack); err != nil {
		return err
	}

	return nil
}

func cleanTemporalSchedules(ctx core.Context, temporalClient client.Client, stackName string) error {
	// list schedules
	listView, _ := temporalClient.ScheduleClient().List(ctx, client.ScheduleListOptions{
		PageSize: 1,
		Query:    fmt.Sprintf("Stack=\"%s\"", stackName),
	})

	for listView.HasNext() {
		s, err := listView.Next()
		if err != nil {
			return err
		}

		// get handle
		handle := temporalClient.ScheduleClient().GetHandle(ctx, s.ID)

		// delete schedule
		handle.Delete(ctx)
	}

	return nil
}

func cleanTemporalWorkflows(ctx core.Context, temporalClient client.Client, temporalNamespace, stackName string) error {
	var nextPageToken []byte
	wg := sync.WaitGroup{}
	for {
		resp, err := temporalClient.WorkflowService().ListWorkflowExecutions(
			ctx,
			&workflowservice.ListWorkflowExecutionsRequest{
				Namespace:     temporalNamespace,
				PageSize:      500,
				NextPageToken: nextPageToken,
				Query:         fmt.Sprintf("Stack=\"%s\"", stackName),
			},
		)
		if err != nil {
			return err
		}

		for _, e := range resp.Executions {
			if e.Status != enums.WORKFLOW_EXECUTION_STATUS_RUNNING {
				continue
			}

			wg.Add(1)
			go func() {
				defer wg.Done()

				// close workflow
				_, err := temporalClient.WorkflowService().TerminateWorkflowExecution(
					ctx,
					&workflowservice.TerminateWorkflowExecutionRequest{
						Namespace:         temporalNamespace,
						WorkflowExecution: e.Execution,
						Reason:            "stack delete",
					},
				)
				if err != nil {
					log.FromContext(ctx).WithValues("workflow", e.Execution.GetWorkflowId()).Error(err, "failed to terminate workflow")
					return
				}
			}()
		}

		if resp.NextPageToken == nil {
			break
		}

		nextPageToken = resp.NextPageToken
	}

	wg.Wait()

	return nil
}

func getTemporalClient(ctx core.Context, stackName string) (string, client.Client, error) {
	temporalURI, err := settings.GetURL(ctx, stackName, "temporal", "dsn")
	if err != nil {
		return "", nil, err
	}

	if temporalURI == nil {
		// No temporal, nothing to clean
		return "", nil, nil
	}

	var temporalTLSKey string
	var temporalTLSCrt string
	if secret := temporalURI.Query().Get("secret"); secret == "" {
		var err error
		temporalTLSCrt, err = settings.GetStringOrEmpty(ctx, stackName, "temporal", "tls", "crt")
		if err != nil {
			return "", nil, err
		}

		temporalTLSKey, err = settings.GetStringOrEmpty(ctx, stackName, "temporal", "tls", "key")
		if err != nil {
			return "", nil, err
		}

	} else {
		s := &corev1.Secret{}
		if err := ctx.GetClient().Get(ctx, types.NamespacedName{
			Namespace: stackName,
			Name:      secret,
		}, s); err != nil {
			return "", nil, err
		}

		temporalTLSCrt = string(s.Data["tls.crt"])
		temporalTLSKey = string(s.Data["tls.key"])
	}

	var cert *tls.Certificate
	if temporalTLSKey != "" && temporalTLSCrt != "" {
		clientCert, err := tls.X509KeyPair([]byte(temporalTLSCrt), []byte(temporalTLSKey))
		if err != nil {
			return "", nil, err
		}
		cert = &clientCert
	}

	options := client.Options{
		Namespace: temporalURI.Path[1:],
		HostPort:  temporalURI.Host,
	}
	if cert != nil {
		options.ConnectionOptions = client.ConnectionOptions{
			TLS: &tls.Config{Certificates: []tls.Certificate{*cert}},
		}
	}

	c, err := client.Dial(options)
	if err != nil {
		return "", nil, err
	}

	return temporalURI.Path[1:], c, nil
}

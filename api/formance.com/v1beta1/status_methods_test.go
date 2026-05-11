package v1beta1

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type readyObject interface {
	SetReady(bool)
	IsReady() bool
	SetError(string)
	GetConditions() *Conditions
}

func TestReadyObjectsStatusMethods(t *testing.T) {
	t.Parallel()

	objects := []readyObject{
		&Auth{}, &AuthClient{}, &Benthos{}, &BenthosStream{}, &Broker{}, &BrokerConsumer{},
		&BrokerTopic{}, &Database{}, &Gateway{}, &GatewayHTTPAPI{}, &Ledger{}, &Orchestration{},
		&Payments{}, &Reconciliation{}, &ResourceReference{}, &Search{}, &Stack{}, &Stargate{},
		&TransactionPlane{}, &Wallets{}, &Webhooks{},
	}

	for _, object := range objects {
		object.SetReady(true)
		object.SetError("ready")
		object.GetConditions().AppendOrReplace(*NewCondition("Ready", 1), ConditionTypeMatch("Ready"))

		require.True(t, object.IsReady())
		require.NotNil(t, object.GetConditions().Get("Ready"))
	}
}

func TestStackAndSettingsMethods(t *testing.T) {
	t.Parallel()

	stack := &Stack{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{SkipLabel: "true"},
		},
		Spec: StackSpec{Version: "v1.2.3"},
	}

	require.True(t, stack.MustSkip())
	require.Equal(t, "v1.2.3", stack.GetVersion())

	settings := &Settings{Spec: SettingsSpec{Stacks: []string{"*"}}}
	require.Equal(t, []string{"*"}, settings.GetStacks())
	require.True(t, settings.IsWildcard())

	settings.Spec.Stacks = []string{"stack0", "stack1"}
	require.False(t, settings.IsWildcard())
}

func TestModuleMethods(t *testing.T) {
	t.Parallel()

	auth := Auth{Spec: AuthSpec{StackDependency: StackDependency{Stack: "stack0"}, ModuleProperties: ModuleProperties{Version: "v1", DevProperties: DevProperties{Debug: true, Dev: true}}}}
	require.True(t, auth.IsEE())
	require.Equal(t, "v1", (&auth).GetVersion())
	require.Equal(t, "stack0", auth.GetStack())
	require.True(t, auth.IsDebug())
	require.True(t, auth.IsDev())

	gateway := Gateway{Spec: GatewaySpec{StackDependency: StackDependency{Stack: "stack0"}, ModuleProperties: ModuleProperties{Version: "v1", DevProperties: DevProperties{Debug: true, Dev: true}}}}
	require.False(t, gateway.IsEE())
	require.Equal(t, "v1", (&gateway).GetVersion())
	require.Equal(t, "stack0", gateway.GetStack())
	require.True(t, gateway.IsDebug())
	require.True(t, gateway.IsDev())

	ledger := Ledger{Spec: LedgerSpec{StackDependency: StackDependency{Stack: "stack0"}, ModuleProperties: ModuleProperties{Version: "v1", DevProperties: DevProperties{Debug: true, Dev: true}}}}
	require.False(t, ledger.IsEE())
	require.Equal(t, "v1", (&ledger).GetVersion())
	require.Equal(t, "stack0", ledger.GetStack())
	require.True(t, ledger.IsDebug())
	require.True(t, ledger.IsDev())
	ledger.isEventPublisher()

	orchestration := Orchestration{Spec: OrchestrationSpec{StackDependency: StackDependency{Stack: "stack0"}, ModuleProperties: ModuleProperties{Version: "v1", DevProperties: DevProperties{Debug: true, Dev: true}}}}
	require.True(t, orchestration.IsEE())
	require.Equal(t, "v1", (&orchestration).GetVersion())
	require.Equal(t, "stack0", orchestration.GetStack())
	require.True(t, orchestration.IsDebug())
	require.True(t, orchestration.IsDev())
	orchestration.isEventPublisher()

	payments := Payments{Spec: PaymentsSpec{StackDependency: StackDependency{Stack: "stack0"}, ModuleProperties: ModuleProperties{Version: "v1", DevProperties: DevProperties{Debug: true, Dev: true}}}}
	require.False(t, payments.IsEE())
	require.Equal(t, "v1", (&payments).GetVersion())
	require.Equal(t, "stack0", payments.GetStack())
	require.True(t, payments.IsDebug())
	require.True(t, payments.IsDev())
	payments.isEventPublisher()

	reconciliation := Reconciliation{Spec: ReconciliationSpec{StackDependency: StackDependency{Stack: "stack0"}, ModuleProperties: ModuleProperties{Version: "v1", DevProperties: DevProperties{Debug: true, Dev: true}}}}
	require.True(t, reconciliation.IsEE())
	require.Equal(t, "v1", (&reconciliation).GetVersion())
	require.Equal(t, "stack0", reconciliation.GetStack())
	require.True(t, reconciliation.IsDebug())
	require.True(t, reconciliation.IsDev())

	search := Search{Spec: SearchSpec{StackDependency: StackDependency{Stack: "stack0"}, ModuleProperties: ModuleProperties{Version: "v1", DevProperties: DevProperties{Debug: true, Dev: true}}}}
	require.True(t, search.IsEE())
	require.Equal(t, "v1", (&search).GetVersion())
	require.Equal(t, "stack0", search.GetStack())
	require.True(t, search.IsDebug())
	require.True(t, search.IsDev())

	stargate := Stargate{Spec: StargateSpec{StackDependency: StackDependency{Stack: "stack0"}, ModuleProperties: ModuleProperties{Version: "v1", DevProperties: DevProperties{Debug: true, Dev: true}}}}
	require.False(t, stargate.IsEE())
	require.Equal(t, "v1", (&stargate).GetVersion())
	require.Equal(t, "stack0", stargate.GetStack())
	require.True(t, stargate.IsDebug())
	require.True(t, stargate.IsDev())

	transactionPlane := TransactionPlane{Spec: TransactionPlaneSpec{StackDependency: StackDependency{Stack: "stack0"}, ModuleProperties: ModuleProperties{Version: "v1", DevProperties: DevProperties{Debug: true, Dev: true}}}}
	require.False(t, transactionPlane.IsEE())
	require.Equal(t, "v1", (&transactionPlane).GetVersion())
	require.Equal(t, "stack0", transactionPlane.GetStack())
	require.True(t, transactionPlane.IsDebug())
	require.True(t, transactionPlane.IsDev())
	transactionPlane.isEventPublisher()

	wallets := Wallets{Spec: WalletsSpec{StackDependency: StackDependency{Stack: "stack0"}, ModuleProperties: ModuleProperties{Version: "v1", DevProperties: DevProperties{Debug: true, Dev: true}}}}
	require.True(t, wallets.IsEE())
	require.Equal(t, "v1", (&wallets).GetVersion())
	require.Equal(t, "stack0", wallets.GetStack())
	require.True(t, wallets.IsDebug())
	require.True(t, wallets.IsDev())

	webhooks := Webhooks{Spec: WebhooksSpec{StackDependency: StackDependency{Stack: "stack0"}, ModuleProperties: ModuleProperties{Version: "v1", DevProperties: DevProperties{Debug: true, Dev: true}}}}
	require.True(t, webhooks.IsEE())
	require.Equal(t, "v1", (&webhooks).GetVersion())
	require.Equal(t, "stack0", webhooks.GetStack())
	require.True(t, webhooks.IsDebug())
	require.True(t, webhooks.IsDev())
}

func TestDependentAndResourceGetStackMethods(t *testing.T) {
	t.Parallel()

	require.Equal(t, "stack0", AuthClient{Spec: AuthClientSpec{StackDependency: StackDependency{Stack: "stack0"}}}.GetStack())
	require.Equal(t, "stack0", Benthos{Spec: BenthosSpec{StackDependency: StackDependency{Stack: "stack0"}}}.GetStack())
	require.Equal(t, "stack0", BenthosStream{Spec: BenthosStreamSpec{StackDependency: StackDependency{Stack: "stack0"}}}.GetStack())
	require.Equal(t, "stack0", (&Broker{Spec: BrokerSpec{StackDependency: StackDependency{Stack: "stack0"}}}).GetStack())
	require.Equal(t, "stack0", (&BrokerConsumer{Spec: BrokerConsumerSpec{StackDependency: StackDependency{Stack: "stack0"}}}).GetStack())
	topic := &BrokerTopic{Spec: BrokerTopicSpec{StackDependency: StackDependency{Stack: "stack0"}}}
	require.Equal(t, "stack0", topic.GetStack())
	topic.isResource()
	database := &Database{Spec: DatabaseSpec{StackDependency: StackDependency{Stack: "stack0"}}}
	require.Equal(t, "stack0", database.GetStack())
	database.isResource()
	require.Equal(t, "stack0", GatewayHTTPAPI{Spec: GatewayHTTPAPISpec{StackDependency: StackDependency{Stack: "stack0"}}}.GetStack())
	require.Equal(t, "stack0", (&ResourceReference{Spec: ResourceReferenceSpec{StackDependency: StackDependency{Stack: "stack0"}}}).GetStack())
}

package v1beta1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestConditionsAppendReplaceDeleteAndCheck(t *testing.T) {
	t.Parallel()

	conditions := Conditions{}
	conditions.AppendOrReplace(*NewCondition("Ready", 1).SetReason("B"), ConditionTypeMatch("Ready"))
	conditions.AppendOrReplace(*NewCondition("Ready", 2).SetReason("A"), ConditionTypeMatch("Ready"))
	conditions.AppendOrReplace(*NewCondition("Synced", 2).SetReason("A"), ConditionTypeMatch("Synced"))

	require.Len(t, conditions, 2)
	require.Equal(t, "Ready", conditions[0].Type)
	require.Equal(t, int64(2), conditions[0].ObservedGeneration)
	require.True(t, conditions.Check(AndConditions(
		ConditionTypeMatch("Ready"),
		ConditionReasonMatch("A"),
		ConditionGenerationMatch(2),
	)))
	require.NotNil(t, conditions.Get("Synced"))

	conditions.Delete(ConditionTypeMatch("Ready"))
	require.Len(t, conditions, 1)
	require.Nil(t, conditions.Get("Ready"))
}

func TestConditionStatusHelpers(t *testing.T) {
	t.Parallel()

	condition := NewCondition("Ready", 3).SetReason("Spec").Fail("not ready")
	require.Equal(t, metav1.ConditionFalse, condition.Status)
	require.Equal(t, "not ready", condition.Message)
	require.Equal(t, "Spec", condition.Reason)

	status := Status{}
	status.SetReady(true)
	status.SetError("ok")
	require.True(t, status.Ready)
	require.Equal(t, "ok", status.Info)
}

func TestModulePropertiesCompareVersion(t *testing.T) {
	t.Parallel()

	stack := &Stack{Spec: StackSpec{Version: "v2.0.0"}}
	require.Equal(t, 0, (&ModuleProperties{}).CompareVersion(stack, "v2.0.0"))
	require.Equal(t, 1, (&ModuleProperties{Version: "v2.1.0"}).CompareVersion(stack, "v2.0.0"))
	require.Equal(t, -1, (&ModuleProperties{Version: "v1.9.0"}).CompareVersion(stack, "v2.0.0"))
	require.Equal(t, 1, (&ModuleProperties{Version: "main"}).CompareVersion(stack, "v2.0.0"))
}

func TestURIJSONAndWithoutQuery(t *testing.T) {
	t.Parallel()

	uri, err := ParseURL("postgresql://user:pass@example.com:5432/db?secret=value")
	require.NoError(t, err)

	data, err := json.Marshal(uri)
	require.NoError(t, err)
	require.JSONEq(t, `"postgresql://user:pass@example.com:5432/db?secret=value"`, string(data))

	var decoded URI
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Equal(t, uri.String(), decoded.String())
	require.Equal(t, "postgresql://user:pass@example.com:5432/db", uri.WithoutQuery().String())
	require.False(t, uri.IsZero())
	require.Equal(t, "nil", URI{}.String())
}

func TestGatewayHosts(t *testing.T) {
	t.Parallel()

	ingress := GatewayIngress{
		Host:  "primary.example.com",
		Hosts: []string{"", "secondary.example.com", "primary.example.com"},
	}

	require.Equal(t, []string{"primary.example.com", "secondary.example.com"}, ingress.GetHosts())
	require.Equal(t, []string{"a", "b"}, DedupHosts([]string{"a", "", "b", "a"}))
}

func TestAuthClientSpecMarshalYAMLShape(t *testing.T) {
	t.Parallel()

	withSecret, err := yaml.Marshal(AuthClientSpec{ID: "client", Secret: "secret"})
	require.NoError(t, err)
	require.Contains(t, string(withSecret), "secrets:\n    - secret")

	withoutSecret, err := yaml.Marshal(AuthClientSpec{ID: "public"})
	require.NoError(t, err)
	require.Contains(t, string(withoutSecret), "secrets: []")
}

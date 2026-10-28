package jobs

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
)

func TestName(t *testing.T) {
	t.Parallel()

	owner := &v1beta1.Broker{
		ObjectMeta: metav1.ObjectMeta{
			UID: types.UID("120b264e-5be3-49a8-bd4a-71e2ea097ae3"),
		},
	}

	tests := []struct {
		name    string
		jobName string
		want    string
	}{
		{
			name:    "short name is unchanged",
			jobName: "create-stream",
			want:    "120b264e-5be3-49a8-bd4a-71e2ea097ae3-create-stream",
		},
		{
			name:    "63-byte name is unchanged",
			jobName: "create-topic-reconciliatio",
			want:    "120b264e-5be3-49a8-bd4a-71e2ea097ae3-create-topic-reconciliatio",
		},
		{
			name:    "64-byte name is shortened",
			jobName: "create-topic-reconciliation",
		},
		{
			name:    "much longer name is shortened",
			jobName: "create-topic-transactionplane-with-a-very-long-suffix",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := Name(owner, tt.jobName)
			if tt.want != "" && got != tt.want {
				t.Fatalf("Name() = %q, want %q", got, tt.want)
			}
			if len(got) > maxNameLength {
				t.Fatalf("Name() = %q is %d bytes, want at most %d", got, len(got), maxNameLength)
			}
			if errs := validation.IsValidLabelValue(got); len(errs) > 0 {
				t.Fatalf("Name() = %q is not a valid label value: %v", got, errs)
			}
			if errs := validation.IsDNS1123Subdomain(got); len(errs) > 0 {
				t.Fatalf("Name() = %q is not a valid object name: %v", got, errs)
			}
			if !strings.HasPrefix(got, string(owner.GetUID())+"-") {
				t.Fatalf("Name() = %q does not start with the owner UID", got)
			}
			if again := Name(owner, tt.jobName); again != got {
				t.Fatalf("Name() is not stable: %q then %q", got, again)
			}
		})
	}
}

func TestNameDistinguishesLongNamesWithSamePrefix(t *testing.T) {
	t.Parallel()

	owner := &v1beta1.Broker{
		ObjectMeta: metav1.ObjectMeta{
			UID: types.UID("120b264e-5be3-49a8-bd4a-71e2ea097ae3"),
		},
	}

	first := Name(owner, "create-topic-reconciliation-a")
	second := Name(owner, "create-topic-reconciliation-b")
	if first == second {
		t.Fatalf("Name() returned %q for two different job names", first)
	}
}

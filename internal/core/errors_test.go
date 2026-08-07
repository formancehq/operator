package core

import (
	"fmt"
	"testing"
	"time"
)

func TestApplicationErrorRequeueAfter(t *testing.T) {
	delay := 5 * time.Second
	err := fmt.Errorf("wrapped: %w", NewPendingError().WithRequeueAfter(delay))

	if got := ApplicationErrorRequeueAfter(err); got != delay {
		t.Fatalf("unexpected requeue delay: got %s, want %s", got, delay)
	}
	if got := ApplicationErrorRequeueAfter(fmt.Errorf("ordinary error")); got != 0 {
		t.Fatalf("ordinary errors must not request a requeue, got %s", got)
	}
}

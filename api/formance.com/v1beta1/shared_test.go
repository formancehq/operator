package v1beta1

import "testing"

// Regression test for a Go 1.26 net/url behavior change (GODEBUG urlstrictcolons):
// a multi-host NATS cluster DSN, stored as a Broker's status.uri, must still
// unmarshal without error (or panic) once it round-trips through JSON.
func TestURIUnmarshalJSONMultiHostNats(t *testing.T) {
	dsn := `"nats://nats-01.example.com:4222,nats-02.example.com:4222,nats-03.example.com:4222/?replicas=3"`

	u := &URI{}
	if err := u.UnmarshalJSON([]byte(dsn)); err != nil {
		t.Fatalf("UnmarshalJSON returned an error for a multi-host NATS DSN: %v", err)
	}

	expectedHost := "nats-01.example.com:4222,nats-02.example.com:4222,nats-03.example.com:4222"
	if u.Host != expectedHost {
		t.Fatalf("Host = %q, want %q", u.Host, expectedHost)
	}
	if port := u.Port(); port != "4222" {
		t.Fatalf("Port() = %q, want %q", port, "4222")
	}
}

func TestURIUnmarshalJSONInvalidURLReturnsErrorWithoutPanic(t *testing.T) {
	u := &URI{}
	err := u.UnmarshalJSON([]byte(`"http://[::1"`))
	if err == nil {
		t.Fatal("expected an error for a malformed URL, got nil")
	}
}

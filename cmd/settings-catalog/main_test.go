package main

import "testing"

func TestScanCatalogFromCode(t *testing.T) {
	t.Parallel()

	c, err := newScanner("../..").scan()
	if err != nil {
		t.Fatal(err)
	}

	if len(c.Settings) == 0 {
		t.Fatal("expected settings to be discovered from code")
	}

	assertHasKey := func(key string) {
		t.Helper()
		for _, setting := range c.Settings {
			if setting.Key == key {
				return
			}
		}
		t.Fatalf("expected key %q to be present", key)
	}

	assertHasKey("auth.<module-name>.check-scopes")
	assertHasKey("aws.service-account")
	assertHasKey("deployments.<deployment-name>.containers.<container-name>.resource-requirements.limits")
	assertHasKey("deployments.<deployment-name>.containers.<container-name>.resource-requirements.requests")
	assertHasKey("jobs.<owner-kind>.containers.<container-name>.run-as")
	assertHasKey("modules.<module-name>.database.connection-pool")
	assertHasKey("opentelemetry.<monitoring-type>.dsn")
	assertHasKey("registries.<name>.images.<path>.rewrite")
}

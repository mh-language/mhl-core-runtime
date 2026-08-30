package external

import (
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/extension"
)

func TestDescribeReturnsHandshakeDeclarations(t *testing.T) {
	m := fakeManifest(t, Permissions{}, []string{"FAKE_DECLARE=1"})

	specs, err := Describe(m)
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if len(specs) != 1 || specs[0].Kind != "fake" || len(specs[0].Methods) != 1 || specs[0].Methods[0].Name != "calls" {
		t.Fatalf("unexpected declarations: %+v", specs)
	}
}

func TestDescribeEmptyWhenExtensionDoesNotSelfDescribe(t *testing.T) {
	specs, err := Describe(fakeManifest(t, Permissions{}, nil))
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if len(specs) != 0 {
		t.Fatalf("expected no declarations, got %+v", specs)
	}
}

func TestSmokePassesForAProtocolCompliantExtension(t *testing.T) {
	m := fakeManifest(t, Permissions{}, nil)
	// Declare a method the fake handles argument-free.
	m.Declares = []extension.DeclarationSpec{{
		Kind:    "fake",
		Methods: []extension.MethodSpec{{Name: "calls"}},
	}}

	results, err := Smoke(m)
	if err != nil {
		t.Fatalf("Smoke: %v", err)
	}
	if len(results) != 1 || !results[0].OK {
		t.Fatalf("expected the smoke test to pass: %+v", results)
	}
}

func TestSmokeCountsAStructuredErrorAsProtocolCompliant(t *testing.T) {
	m := fakeManifest(t, Permissions{}, nil)
	m.Declares = []extension.DeclarationSpec{{
		Kind:    "fake",
		Methods: []extension.MethodSpec{{Name: "no_such_op"}}, // fake replies with a wireError
	}}

	results, err := Smoke(m)
	if err != nil {
		t.Fatalf("Smoke: %v", err)
	}
	if len(results) != 1 || !results[0].OK || !strings.Contains(results[0].Detail, "structured error") {
		t.Fatalf("a structured error should count as compliant: %+v", results)
	}
}

func TestSmokeFailsWhenTheMethodCrashesTheProcess(t *testing.T) {
	m := fakeManifest(t, Permissions{}, nil)
	m.Declares = []extension.DeclarationSpec{{
		Kind:    "fake",
		Methods: []extension.MethodSpec{{Name: "boom"}}, // os.Exit(3)
	}}

	results, err := Smoke(m)
	if err != nil {
		t.Fatalf("Smoke: %v", err)
	}
	if len(results) != 1 || results[0].OK {
		t.Fatalf("a method that crashes the process must fail the smoke test: %+v", results)
	}
}

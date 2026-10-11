package external

import (
	"context"
	"testing"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/extension"
)

// Two installed extensions serving the same kind run side by side; a
// declaration's Provider (`from "<id>"`) routes each call to its own process.
func TestRegistryRoutesSameKindByProvider(t *testing.T) {
	manifest := func(id, tag string) *Manifest {
		m := fakeManifest(t, Permissions{}, []string{"FAKE_TAG=" + tag})
		m.ID = id
		return m
	}
	a, b := New(manifest("com.test.fake.a", "a")), New(manifest("com.test.fake.b", "b"))
	t.Cleanup(func() { a.Close(); b.Close() })

	reg := extension.NewRegistry(&recordingHost{})
	for _, ext := range []*External{a, b} {
		if err := reg.TryRegister(ext); err != nil {
			t.Fatalf("TryRegister %s: %v", ext.ID(), err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for provider, want := range map[string]string{"com.test.fake.a": "a", "com.test.fake.b": "b"} {
		got, err := reg.Call(ctx, extension.CallRequest{
			Declaration: extension.Declaration{Kind: "fake", Provider: provider, Name: "F"},
			Method:      "tag",
		})
		if err != nil {
			t.Fatalf("Call via %s: %v", provider, err)
		}
		if got != want {
			t.Fatalf("Call via %s = %v, want %q", provider, got, want)
		}
	}

	if _, err := reg.Resolve(extension.Declaration{Kind: "fake", Name: "F"}); err == nil {
		t.Fatal("expected an ambiguity error for kind \"fake\" without a provider")
	}
}

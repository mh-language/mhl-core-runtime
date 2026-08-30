package external

import (
	"context"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/extension"
)

// These benchmarks stand in for the phase-0 baseline the plan's §7 targets
// refer to: warm-call overhead should stay well under a local network call,
// and no benchmark iteration may spawn a process after the first.

func benchInstance(b *testing.B) (*External, extension.Instance) {
	b.Helper()
	m := &Manifest{
		ID: "com.test.fake", APIVersion: APIVersion,
		Executable: benchExe(b), Args: []string{"-test.run=^$"},
		Env:      []string{"GO_WANT_HELPER_PROCESS=1"},
		Declares: []extension.DeclarationSpec{{Kind: "fake"}},
	}
	ext := New(m)
	inst, err := ext.Bind(extension.Declaration{Kind: "fake", Name: "F"}, &recordingHost{})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { ext.Close() })
	return ext, inst
}

func benchExe(b *testing.B) string {
	b.Helper()
	// os.Args[0] is the test binary; the helper-process gate in TestMain
	// turns it into the fake extension.
	return testBinary
}

func BenchmarkExternalWarmCall(b *testing.B) {
	_, inst := benchInstance(b)
	ctx := context.Background()
	req := extension.CallRequest{Declaration: extension.Declaration{Kind: "fake", Name: "F"}, Method: "echo", Args: []extension.Value{"x"}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := inst.Call(ctx, req); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkExternalWarmCallParallel(b *testing.B) {
	_, inst := benchInstance(b)
	ctx := context.Background()
	req := extension.CallRequest{Declaration: extension.Declaration{Kind: "fake", Name: "F"}, Method: "echo", Args: []extension.Value{"x"}}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := inst.Call(ctx, req); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkExternalLargePayload(b *testing.B) {
	_, inst := benchInstance(b)
	ctx := context.Background()
	big := strings.Repeat("payload-", 8192) // ~64 KiB, echoed back
	req := extension.CallRequest{Declaration: extension.Declaration{Kind: "fake", Name: "F"}, Method: "echo", Args: []extension.Value{big}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := inst.Call(ctx, req); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkExternalColdStart(b *testing.B) {
	for i := 0; i < b.N; i++ {
		m := &Manifest{
			ID: "com.test.fake", APIVersion: APIVersion,
			Executable: testBinary, Args: []string{"-test.run=^$"},
			Env:      []string{"GO_WANT_HELPER_PROCESS=1"},
			Declares: []extension.DeclarationSpec{{Kind: "fake"}},
		}
		ext := New(m)
		inst, _ := ext.Bind(extension.Declaration{Kind: "fake", Name: "F"}, &recordingHost{})
		if _, err := inst.Call(context.Background(), extension.CallRequest{
			Declaration: extension.Declaration{Kind: "fake", Name: "F"}, Method: "echo", Args: []extension.Value{"x"},
		}); err != nil {
			b.Fatal(err)
		}
		ext.Close()
	}
}

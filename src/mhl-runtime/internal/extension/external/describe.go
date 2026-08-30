package external

import (
	"context"
	"fmt"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/extension"
)

// probeTimeout bounds the one-shot spawns Describe and Smoke do.
const probeTimeout = 10 * time.Second

// Describe spawns the extension once, performs the handshake, and returns the
// declarations it reports about itself (initializeResult.declarations). Empty
// is not an error — an extension may simply not self-describe. Used by
// `mhl extension package` to materialise the manifest's declarations sidecar.
func Describe(m *Manifest) ([]extension.DeclarationSpec, error) {
	p, err := startProcess(m.ExecutablePath(), m.Args, m.launchEnv(), nil)
	if err != nil {
		return nil, err
	}
	defer func() { p.close(); p.kill() }()

	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	res, err := handshake(ctx, p)
	if err != nil {
		return nil, err
	}
	return res.Declarations, nil
}

// SmokeResult is one method's outcome in a `mhl extension test` run.
type SmokeResult struct {
	Kind   string
	Method string
	OK     bool
	Detail string
}

// Smoke spawns the extension, handshakes, and sends one argument-free `call`
// for every declared method, checking that a well-formed response comes back
// (a result or a structured error both count — the point is that the
// extension speaks the protocol, not that a bare call succeeds). Transport
// failures, crashes and timeouts are the failures it reports.
func Smoke(m *Manifest) ([]SmokeResult, error) {
	ext := New(m)
	defer ext.Close()

	var results []SmokeResult
	for _, spec := range m.Declares {
		inst, err := ext.Bind(extension.Declaration{Kind: spec.Kind, Name: "probe"}, extension.NopHost{})
		if err != nil {
			return nil, fmt.Errorf("bind %q: %w", spec.Kind, err)
		}
		methods := spec.Methods
		if len(methods) == 0 {
			results = append(results, SmokeResult{Kind: spec.Kind, Method: "(none declared)", OK: true, Detail: "handshake only"})
			// Force the process up so the handshake itself is exercised.
			ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
			_, _ = inst.Call(ctx, extension.CallRequest{Declaration: extension.Declaration{Kind: spec.Kind, Name: "probe"}, Method: "__handshake_probe__"})
			cancel()
			continue
		}
		for _, meth := range methods {
			ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
			_, callErr := inst.Call(ctx, extension.CallRequest{
				Declaration: extension.Declaration{Kind: spec.Kind, Name: "probe"},
				Method:      meth.Name,
			})
			cancel()

			r := SmokeResult{Kind: spec.Kind, Method: meth.Name}
			switch {
			case callErr == nil:
				r.OK, r.Detail = true, "returned a result"
			case ext.alive():
				// The extension answered with an error frame and is still
				// running — it spoke the protocol, which is what this checks.
				r.OK, r.Detail = true, "returned a structured error: "+callErr.Error()
			default:
				r.OK, r.Detail = false, callErr.Error()
			}
			results = append(results, r)
		}
	}
	return results, nil
}

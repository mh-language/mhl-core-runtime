package interpreter

import "context"

type noAgentCacheKey struct{}

// WithoutAgentCache returns ctx marking the run under it as one that must
// not reuse an agent's cached response (`mhl run --no-cache`,
// execsvc.Request.NoCache): every agent call is made again, and its fresh
// response replaces the cached one, so a later run without the flag reuses
// that instead of a stale reply. Agents without a `cache` property are
// unaffected.
func WithoutAgentCache(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, noAgentCacheKey{}, true)
}

// agentCacheBypassed reports whether ctx carries WithoutAgentCache.
func agentCacheBypassed(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(noAgentCacheKey{}).(bool)
	return v
}

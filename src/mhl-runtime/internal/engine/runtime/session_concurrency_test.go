package runtime_test

import (
	"sync"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
)

// TestConcurrentSaveAcrossSessionsOfTheSamePipelineNeverErrors reproduces a
// real failure: a side-effect-free workflow (e.g. ArtifactPreview) called
// once per item of a batch instead of once for the whole batch runs several
// sessions of the *same* pipeline name concurrently. Each session's own
// checkpoint file is already isolated (Store.Session scopes it under
// <base>/<sessionID>/), but every one of them also updates the pipeline's
// shared ".latest" pointer under <base>/ — and that used a single
// unconditional "pipeline.latest.tmp" temp file for every writer, so one
// session's os.Rename could find another had already renamed the same tmp
// file away first: "runtime: committing latest pointer: rename
// .../P.latest.tmp .../P.latest: no such file or directory". Seen for real
// against the MCP server, not just in theory.
func TestConcurrentSaveAcrossSessionsOfTheSamePipelineNeverErrors(t *testing.T) {
	base := runtime.NewStore(t.TempDir())

	const n = 20
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sess := base.Session(runtime.NewSessionID())
			errs <- sess.Save(&runtime.Checkpoint{Pipeline: "ArtifactPreview", LastStep: "Dispatch", NextStep: "Done"})
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Errorf("concurrent Save on the same pipeline name failed: %v", err)
		}
	}
}

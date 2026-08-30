package external

import "sync"

// stderrTailMax is how many trailing bytes of an extension's stderr the host
// keeps — enough to quote a stack trace or panic line in an error, not so
// much that a chatty extension grows the host's memory without bound.
const stderrTailMax = 16 << 10

// stderrTail is an io.Writer that retains only the last stderrTailMax bytes
// written to it. The extension's stderr goes here, kept apart from the
// JSON-RPC stream on stdout, and is quoted into a call error when the process
// dies.
type stderrTail struct {
	mu  sync.Mutex
	buf []byte
}

func (s *stderrTail) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = append(s.buf, p...)
	if len(s.buf) > stderrTailMax {
		s.buf = s.buf[len(s.buf)-stderrTailMax:]
	}
	return len(p), nil
}

func (s *stderrTail) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.buf)
}

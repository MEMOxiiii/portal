package socket

import "testing"

// TestAuthThrottleCloseTwice guards against a regression where Close() called close(t.stop) unconditionally,
// panicking if a caller (e.g. a DefaultServer.Close() invoked more than once by an embedder) closed the
// throttle twice.
func TestAuthThrottleCloseTwice(t *testing.T) {
	th := newAuthThrottle()
	th.Close()
	th.Close()
}

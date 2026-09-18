package log

import (
	"bytes"
	"os"
	"testing"
)

// TestLoggerWriteReturnsLenP guards against a regression where Write returned the byte count from writing
// the transformed (timestamped, ANSI-stripped) line to the log file, instead of len(p) as io.Writer
// requires -- callers that check the returned n against len(p) (unlike logrus, the only current caller)
// would see a spurious short/long write.
func TestLoggerWriteReturnsLenP(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "portal-log")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var stdout bytes.Buffer
	l := &Logger{file: f, stdout: &stdout}

	p := []byte("hello world\n")
	n, err := l.Write(p)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(p) {
		t.Fatalf("Write returned n = %d, want %d (len(p))", n, len(p))
	}
	if stdout.String() != string(p) {
		t.Fatalf("stdout = %q, want %q", stdout.String(), string(p))
	}
}

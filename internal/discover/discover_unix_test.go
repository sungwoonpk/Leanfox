//go:build unix

package discover

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestSomethingThatIsNotAFileIsRefusedAndNeverOpened(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "p", "pipe.jsonl")
	if err := syscall.Mkfifo(filepath.Join(dir, "plain"), 0o600); err != nil {
		t.Skip("named pipes are not available here")
	}
	if err := os.MkdirAll(filepath.Dir(fifo), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	// Named directly, it is refused with a message.
	if _, err := Find(Options{Paths: []string{fifo}}); err == nil {
		t.Error("a pipe named on the command line must be refused")
	}
	// Found inside a folder, it is skipped, and the search does not wait for it to be written to.
	got, err := Find(Options{Root: dir})
	if err != nil || len(got) != 0 {
		t.Errorf("found %v, err %v", got, err)
	}
}

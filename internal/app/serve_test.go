package app

import "testing"

func TestForwardWithoutServer(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	if _, ok := Forward(Request{Op: "ping"}); ok {
		t.Fatal("forward reported a server that is not running")
	}
}

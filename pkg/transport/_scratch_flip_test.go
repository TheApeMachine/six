package transport

import (
	"bytes"
	"io"
	"testing"
)

func TestScratchFlipFlopSecondLeg(t *testing.T) {
	from := bytes.NewBufferString("ping")
	to := &flipFlopFailRead{}

	_, err1 := io.Copy(to, from)
	if err1 != nil {
		t.Fatalf("copy1: %v", err1)
	}

	_, err2 := io.Copy(from, to)
	if err2 == nil {
		t.Fatal("expected copy2 error")
	}
	t.Logf("copy2 err: %v", err2)
}

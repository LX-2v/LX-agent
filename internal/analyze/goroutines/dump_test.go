package goroutines

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseGroupsByStateAndStack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "goroutines.txt")
	dump := "goroutine 1 [chan receive]:\nmain.wait()\n\ngoroutine 2 [chan receive]:\nmain.wait()\n\ngoroutine 3 [running]:\nmain.work()\n"
	if err := os.WriteFile(path, []byte(dump), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Parse(context.Background(), path, 4096, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 3 || len(got.Groups) != 1 || got.Groups[0].Count != 2 || !got.Truncated {
		t.Fatalf("unexpected result: %#v", got)
	}
}

func TestParseRejectsOversizedDump(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.txt")
	if err := os.WriteFile(path, []byte("goroutine 1 [running]:\nmain.work()\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(context.Background(), path, 4, 10); err == nil {
		t.Fatal("expected size error")
	}
}

func TestCommittedGoroutineFixtureAccuracy(t *testing.T) {
	path := filepath.Join("..", "..", "..", "testdata", "goroutines.txt")
	got, err := Parse(context.Background(), path, 64<<20, 3)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, group := range got.Groups {
		if group.Count >= 12 {
			found = true
		}
	}
	if got.Total < 12 || !found {
		t.Fatalf("leaked goroutine group absent from top 3: %#v", got)
	}
}

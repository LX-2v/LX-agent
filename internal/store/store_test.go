package store

import (
	"testing"

	"github.com/google/pprof/profile"
)

func TestByteBudgetEvictsLeastRecentlyUsed(t *testing.T) {
	s, err := New(10)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Put(&profile.Profile{}, 5)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Put(&profile.Profile{}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(a); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Put(&profile.Profile{}, 5); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(b); err != ErrNotFound {
		t.Fatalf("expected LRU eviction, got %v", err)
	}
	if _, err = s.Get(a); err != nil {
		t.Fatalf("recent item evicted: %v", err)
	}
}

package store

import (
	"container/list"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/google/pprof/profile"
)

var ErrNotFound = errors.New("profile not found or evicted")

type entry struct {
	id    string
	p     *profile.Profile
	bytes int64
}
type Store struct {
	mu        sync.Mutex
	max, used int64
	ll        *list.List
	byID      map[string]*list.Element
}

func New(maxBytes int64) (*Store, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("store byte budget must be positive")
	}
	return &Store{max: maxBytes, ll: list.New(), byID: make(map[string]*list.Element)}, nil
}

func (s *Store) Put(p *profile.Profile, size int64) (string, error) {
	if p == nil || size <= 0 || size > s.max {
		return "", fmt.Errorf("profile size %d exceeds store budget %d", size, s.max)
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("create profile id: %w", err)
	}
	id := hex.EncodeToString(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	for s.used+size > s.max {
		e := s.ll.Back()
		old := e.Value.(*entry)
		s.used -= old.bytes
		delete(s.byID, old.id)
		s.ll.Remove(e)
	}
	e := s.ll.PushFront(&entry{id: id, p: p, bytes: size})
	s.byID[id] = e
	s.used += size
	return id, nil
}

func (s *Store) Get(id string) (*profile.Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.byID[id]
	if e == nil {
		return nil, ErrNotFound
	}
	s.ll.MoveToFront(e)
	return e.Value.(*entry).p, nil
}

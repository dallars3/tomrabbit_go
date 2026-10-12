// Package session holds server-side state attributed to a client across
// requests. Sessions use sliding expiry: a session lives as long as requests
// keep arriving within the idle TTL.
package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Session is one client's server-side state. Methods are safe for concurrent
// use. The zero value must not be used; obtain sessions from a Store.
type Session struct {
	id         string
	mu         sync.RWMutex
	attrs      map[string]any
	lastAccess time.Time
}

// ID returns the JSESSIONID value for this session.
func (s *Session) ID() string { return s.id }

// Get returns an attribute.
func (s *Session) Get(name string) (any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.attrs[name]
	return v, ok
}

// Set stores an attribute.
func (s *Session) Set(name string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attrs[name] = value
}

// Delete removes an attribute.
func (s *Session) Delete(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.attrs, name)
}

func (s *Session) lastAccessedAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastAccess
}

func (s *Session) touch(at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastAccess = at
}

// Store creates and retains sessions, expiring them after an idle period.
// The zero value must not be used; construct with NewStore.
type Store struct {
	ttl time.Duration
	now func() time.Time

	mu    sync.RWMutex
	items map[string]*Session
}

// NewStore creates a Store with sliding-expiry TTL using the wall clock.
func NewStore(ttl time.Duration) *Store {
	return NewStoreWithClock(ttl, time.Now)
}

// NewStoreWithClock creates a Store with an injected clock, used by tests.
func NewStoreWithClock(ttl time.Duration, now func() time.Time) *Store {
	if ttl <= 0 {
		panic("session TTL must be positive")
	}
	return &Store{ttl: ttl, now: now, items: make(map[string]*Session)}
}

// Create starts a new session with a random 128-bit hex id.
func (st *Store) Create() *Session {
	st.mu.Lock()
	defer st.mu.Unlock()
	var s *Session
	for {
		s = &Session{id: newID(), attrs: make(map[string]any), lastAccess: st.now()}
		if _, exists := st.items[s.id]; !exists {
			break
		}
	}
	st.items[s.id] = s
	return s
}

// Get returns the session when the id exists and has not been idle past the
// TTL. Expired sessions are deleted on sight.
func (st *Store) Get(id string) (*Session, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	s, ok := st.items[id]
	if !ok {
		return nil, false
	}
	if st.now().Sub(s.lastAccessedAt()) >= st.ttl {
		delete(st.items, id)
		return nil, false
	}
	return s, true
}

// Touch marks a session as used now, extending its sliding deadline.
func (st *Store) Touch(s *Session) {
	s.touch(st.now())
}

// CleanExpired deletes every session idle for at least the TTL and returns how
// many were removed. Safe to call repeatedly; StartCleanup calls it on a ticker.
func (st *Store) CleanExpired() int {
	now := st.now()
	st.mu.Lock()
	defer st.mu.Unlock()
	removed := 0
	for id, s := range st.items {
		if now.Sub(s.lastAccessedAt()) >= st.ttl {
			delete(st.items, id)
			removed++
		}
	}
	return removed
}

// StartCleanup runs CleanExpired on every tick until the context is cancelled.
func (st *Store) StartCleanup(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			st.CleanExpired()
		}
	}
}

// Len reports how many live sessions are retained; intended for diagnostics.
func (st *Store) Len() int {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return len(st.items)
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing is unrecoverable; without ids sessions cannot
		// be attributed safely.
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

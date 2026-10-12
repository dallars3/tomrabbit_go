package session

import (
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func newTestStore(ttl time.Duration) (*Store, *fakeClock) {
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	return NewStoreWithClock(ttl, clock.now), clock
}

func TestCreateGeneratesUniqueIDs(t *testing.T) {
	st, _ := newTestStore(time.Minute)
	s1 := st.Create()
	s2 := st.Create()
	if s1.ID() == "" || s1.ID() == s2.ID() {
		t.Fatalf("expected two unique non-empty ids, got %q and %q", s1.ID(), s2.ID())
	}
}

func TestGetExistingAndMissing(t *testing.T) {
	st, _ := newTestStore(time.Minute)
	s := st.Create()

	got, ok := st.Get(s.ID())
	if !ok || got != s {
		t.Fatal("expected to retrieve the created session by id")
	}
	if _, ok := st.Get("does-not-exist"); ok {
		t.Fatal("unknown id must not resolve")
	}
}

func TestAttributes(t *testing.T) {
	st, _ := newTestStore(time.Minute)
	s := st.Create()

	if _, ok := s.Get("visits"); ok {
		t.Fatal("missing attribute should report ok=false")
	}
	s.Set("visits", 3)
	v, ok := s.Get("visits")
	if !ok || v.(int) != 3 {
		t.Fatalf("attribute = %v, %v", v, ok)
	}
	s.Delete("visits")
	if _, ok := s.Get("visits"); ok {
		t.Fatal("attribute should be gone after Delete")
	}
}

func TestGetMissesAfterIdleTimeout(t *testing.T) {
	st, clock := newTestStore(30 * time.Minute)
	s := st.Create()

	clock.t = clock.t.Add(31 * time.Minute)
	if _, ok := st.Get(s.ID()); ok {
		t.Fatal("session idle past its TTL must not resolve")
	}
}

func TestTouchImplementsSlidingExpiry(t *testing.T) {
	st, clock := newTestStore(30 * time.Minute)
	s := st.Create()

	clock.t = clock.t.Add(20 * time.Minute)
	st.Touch(s) // request at minute 20 resets the deadline
	clock.t = clock.t.Add(20 * time.Minute)
	if _, ok := st.Get(s.ID()); !ok {
		t.Fatal("session used within TTL of its last touch must survive (minute 40)")
	}

	clock.t = clock.t.Add(11 * time.Minute)
	if _, ok := st.Get(s.ID()); ok {
		t.Fatal("session idle 31 minutes after the touch must expire (minute 51)")
	}
}

func TestCleanupRemovesOnlyExpired(t *testing.T) {
	st, clock := newTestStore(30 * time.Minute)
	young := st.Create()
	clock.t = clock.t.Add(10 * time.Minute)
	middle := st.Create()
	clock.t = clock.t.Add(25 * time.Minute) // young is now at 35 min idle, middle at 25

	removed := st.CleanExpired()
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, ok := st.Get(young.ID()); ok {
		t.Fatal("expired session should be deleted")
	}
	if _, ok := st.Get(middle.ID()); !ok {
		t.Fatal("session still inside its idle window must survive cleanup")
	}
}

func TestAttributesAreConcurrentSafe(t *testing.T) {
	st, _ := newTestStore(time.Minute)
	s := st.Create()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			s.Set("k", i)
		}
		close(done)
	}()
	for i := 0; i < 200; i++ {
		s.Get("k")
	}
	<-done
}

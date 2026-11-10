package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestFBUploadQueuesOnTransientFailureThenDrains reproduces the scenario the queue
// exists for: the server (a VPS reached over the internet) is momentarily
// unreachable when the write is first attempted. fbUpload must not lose the data or
// fail the caller — it queues the write, and the background worker delivers it as
// soon as the server answers again, with no further action from the caller.
func TestFBUploadQueuesOnTransientFailureThenDrains(t *testing.T) {
	fb := newFakeFileBrowser("tok123")
	fb.dirs["users/est"] = true

	// The first upload attempt (the synchronous one made by fbUpload itself) fails
	// with a 503, exactly what an overloaded/restarting VPS would answer. Every
	// later attempt (i.e. the worker's automatic retry) succeeds.
	var failedOnce int32
	inner := fb.handler("est", "segredo")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/resources" &&
			r.URL.Query().Get("isDir") != "true" && atomic.CompareAndSwapInt32(&failedOnce, 0, 1) {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		inner(w, r)
	}))
	defer server.Close()

	a := &App{dataDir: t.TempDir(), fbURL: server.URL, fbToken: fb.token, fbUser: "est"}
	a.fbQueueInit()

	if err := a.fbUpload("users/est/dixitalizacion-backup.json", []byte(`{"ok":true}`)); err != nil {
		t.Fatalf("fbUpload should absorb a transient failure and queue it, got error: %v", err)
	}

	// Right after the queuing, the write must not have reached the server yet...
	if st := a.FBQueueStatus(); st.Pendentes != 1 {
		t.Fatalf("expected 1 pending op right after the transient failure, got %d", st.Pendentes)
	}
	if raw, err := os.ReadFile(filepath.Join(a.dataDir, fbQueueFile)); err != nil || !strings.Contains(string(raw), "dixitalizacion-backup.json") {
		t.Fatalf("pending op should be persisted to disk, read: %q, err: %v", raw, err)
	}

	// ...but the worker retries it in the background and the queue drains on its own.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if a.FBQueueStatus().Pendentes == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st := a.FBQueueStatus(); st.Pendentes != 0 {
		t.Fatalf("queue should have drained once the server recovered, still pending: %d (lastErr=%q)", st.Pendentes, st.UltimoErro)
	}
	if got := string(fb.files["users/est/dixitalizacion-backup.json"]); got != `{"ok":true}` {
		t.Fatalf("server should have received the queued content, got %q", got)
	}
}

// TestFBQueuePersistsAcrossRestart checks that a pending operation survives the app
// closing (e.g. the student shuts down mid-outage) by reloading it from disk into a
// fresh App, as startup does via fbQueueInit.
func TestFBQueuePersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()

	// A server that always fails, so the write never leaves the queue during this
	// first "session".
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	a1 := &App{dataDir: dir, fbURL: server.URL, fbToken: "tok", fbUser: "est"}
	a1.fbQueueInit()
	if err := a1.fbUpload("users/est/dixitalizacion-backup.json", []byte(`{"n":1}`)); err != nil {
		t.Fatalf("fbUpload: %v", err)
	}
	// fbEnqueue persists synchronously before returning, so no need to wait here.
	if st := a1.FBQueueStatus(); st.Pendentes != 1 {
		t.Fatalf("expected 1 pending op, got %d", st.Pendentes)
	}

	// Simulate the app restarting: a brand new App pointed at the same data dir.
	a2 := &App{dataDir: dir}
	a2.fbQueueInit()
	if st := a2.FBQueueStatus(); st.Pendentes != 1 {
		t.Fatalf("pending op should survive a restart, got %d pending", st.Pendentes)
	}
}

// TestFBBackoffGrowsAndCaps checks the retry schedule: it grows with each failed
// attempt but never exceeds 5 minutes, so a real outage is retried often enough to
// recover quickly without hammering the server once it's down for a while.
func TestFBBackoffGrowsAndCaps(t *testing.T) {
	if d := fbBackoff(1); d != 5*time.Second {
		t.Fatalf("first backoff should be 5s, got %v", d)
	}
	prev := fbBackoff(1)
	for attempts := 2; attempts <= 20; attempts++ {
		d := fbBackoff(attempts)
		if d < prev {
			t.Fatalf("backoff should never shrink: attempts=%d gave %v, previous was %v", attempts, d, prev)
		}
		if d > 5*time.Minute {
			t.Fatalf("backoff should be capped at 5m, got %v at attempts=%d", d, attempts)
		}
		prev = d
	}
	if d := fbBackoff(20); d != 5*time.Minute {
		t.Fatalf("backoff should have reached the 5m cap by 20 attempts, got %v", d)
	}
}

// TestFBUploadReturnsDefiniteErrorsImmediately checks that a non-transient failure
// (here: an expired/invalid session, 403) is NOT queued — retrying it would never
// succeed, so the caller must see the error right away instead of a silent retry loop.
func TestFBUploadReturnsDefiniteErrorsImmediately(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	a := &App{dataDir: t.TempDir(), fbURL: server.URL, fbToken: "expired", fbUser: "est"}
	a.fbQueueInit()

	err := a.fbUpload("users/est/dixitalizacion-backup.json", []byte(`{}`))
	if err == nil {
		t.Fatal("a definite (403) failure should be returned to the caller, not swallowed")
	}
	if st := a.FBQueueStatus(); st.Pendentes != 0 {
		t.Fatalf("a definite failure must not be queued, got %d pending", st.Pendentes)
	}
}

// TestFBQueueStatusJSONRoundTrip is a light sanity check that FBQueueStatus (what the
// Wails binding serialises to the frontend) survives a JSON round trip with the field
// names the frontend expects.
func TestFBQueueStatusJSONRoundTrip(t *testing.T) {
	st := FBQueueStatus{Pendentes: 2, UltimoErro: "erro exemplo"}
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got FBQueueStatus
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != st {
		t.Fatalf("round trip mismatch: got %+v, want %+v", got, st)
	}
}

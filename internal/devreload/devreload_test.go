package devreload_test

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/devreload"
)

type stream struct {
	lines chan string
	close func()
}

// subscribe opens the event stream and pumps its lines into a channel, since
// reading a stream that stays open cannot block the test.
func subscribe(t *testing.T, url string) *stream {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", url, resp.StatusCode)
	}
	lines := make(chan string, 64)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()
	s := &stream{lines: lines, close: func() { _ = resp.Body.Close() }}
	t.Cleanup(s.close)
	return s
}

// await returns the data of the next line naming the wanted event.
func (s *stream) await(t *testing.T, event string, timeout time.Duration) string {
	t.Helper()
	deadline := time.After(timeout)
	seen := false
	for {
		select {
		case line, ok := <-s.lines:
			if !ok {
				t.Fatalf("stream closed before event %q", event)
			}
			if line == "event: "+event {
				seen = true
				continue
			}
			if seen && strings.HasPrefix(line, "data: ") {
				return strings.TrimPrefix(line, "data: ")
			}
		case <-deadline:
			t.Fatalf("timed out waiting for event %q", event)
		}
	}
}

func (s *stream) quiet(t *testing.T, event string, d time.Duration) int {
	t.Helper()
	count := 0
	deadline := time.After(d)
	for {
		select {
		case line, ok := <-s.lines:
			if !ok {
				return count
			}
			if line == "event: "+event {
				count++
			}
		case <-deadline:
			return count
		}
	}
}

func serve(t *testing.T, hub *devreload.Hub) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/_dev/reload", hub.Events)
	mux.HandleFunc("/_dev/reload.js", hub.Script)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestEvents_HelloCarriesTheInstanceID(t *testing.T) {
	srv := serve(t, devreload.New("instance-abc"))
	s := subscribe(t, srv.URL+"/_dev/reload")
	if got := s.await(t, "hello", 2*time.Second); got != "instance-abc" {
		t.Errorf("hello data = %q, want %q", got, "instance-abc")
	}
}

// The client reloads when the id changes, so a reconnect to the same server has
// to report the same id — that is what stops a reload looping forever.
func TestEvents_SameServerReportsTheSameIDOnReconnect(t *testing.T) {
	srv := serve(t, devreload.New("instance-abc"))
	first := subscribe(t, srv.URL+"/_dev/reload").await(t, "hello", 2*time.Second)
	second := subscribe(t, srv.URL+"/_dev/reload").await(t, "hello", 2*time.Second)
	if first != second {
		t.Errorf("ids %q then %q, want them equal", first, second)
	}
}

func TestEvents_ARestartReportsADifferentID(t *testing.T) {
	before := subscribe(t, serve(t, devreload.New("before")).URL+"/_dev/reload").
		await(t, "hello", 2*time.Second)
	after := subscribe(t, serve(t, devreload.New("after")).URL+"/_dev/reload").
		await(t, "hello", 2*time.Second)
	if before == after {
		t.Errorf("both servers reported %q, want different ids", before)
	}
}

func TestReload_ReachesEverySubscriber(t *testing.T) {
	hub := devreload.New("instance-abc")
	srv := serve(t, hub)
	one := subscribe(t, srv.URL+"/_dev/reload")
	two := subscribe(t, srv.URL+"/_dev/reload")
	one.await(t, "hello", 2*time.Second)
	two.await(t, "hello", 2*time.Second)

	hub.Reload()

	one.await(t, "reload", 2*time.Second)
	two.await(t, "reload", 2*time.Second)
}

// A browser tab that has stopped reading must not stall the broadcast for the
// tab the developer is actually looking at.
func TestReload_ASubscriberThatNeverReadsDoesNotBlockAnother(t *testing.T) {
	hub := devreload.New("instance-abc")
	srv := serve(t, hub)

	stalled, err := http.Get(srv.URL + "/_dev/reload")
	if err != nil {
		t.Fatalf("stalled subscriber: %v", err)
	}
	defer func() { _ = stalled.Body.Close() }()

	reader := subscribe(t, srv.URL+"/_dev/reload")
	reader.await(t, "hello", 2*time.Second)

	// Enough events to overrun the socket and transport buffers, so the stalled
	// subscriber's handler really does stop draining. Below that the client
	// buffers everything and nothing is being tested.
	for range 50_000 {
		hub.Reload()
	}
	reader.await(t, "reload", 5*time.Second)
}

// http.Server.Shutdown waits for in-flight requests, and a stream never
// finishes on its own, so an open tab would hold every restart open until the
// reloader's watchdog killed the process.
func TestClose_EndsOpenStreamsSoShutdownIsNotHeldUp(t *testing.T) {
	hub := devreload.New("instance-abc")
	srv := serve(t, hub)
	s := subscribe(t, srv.URL+"/_dev/reload")
	s.await(t, "hello", 2*time.Second)

	hub.Close()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-s.lines:
			if !ok {
				return // the stream ended, which is what lets Shutdown return
			}
		case <-deadline:
			t.Fatal("stream still open 2s after Close")
		}
	}
}

func TestScript_IsServedAsJavaScriptAndPointsAtTheStream(t *testing.T) {
	srv := serve(t, devreload.New("instance-abc"))
	resp, err := http.Get(srv.URL + "/_dev/reload.js")
	if err != nil {
		t.Fatalf("GET script: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body := make([]byte, 4096)
	n, _ := resp.Body.Read(body)
	js := string(body[:n])

	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("Content-Type = %q, want javascript", ct)
	}
	for _, want := range []string{"/_dev/reload", "EventSource", "location.reload"} {
		if !strings.Contains(js, want) {
			t.Errorf("script does not mention %q", want)
		}
	}
}

func TestWatch_AnAssetWriteBroadcastsOneReload(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.css"), []byte("a{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	hub := devreload.New("instance-abc")
	srv := serve(t, hub)
	s := subscribe(t, srv.URL+"/_dev/reload")
	s.await(t, "hello", 2*time.Second)

	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		if err := hub.Watch(stop, dir); err != nil {
			panic(err)
		}
	}()
	time.Sleep(300 * time.Millisecond)

	// Two writes in quick succession: a Tailwind build emits several events, and
	// the developer wants one reload, not one per event.
	if err := os.WriteFile(filepath.Join(dir, "main.css"), []byte("b{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.css"), []byte("c{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	s.await(t, "reload", 3*time.Second)
	if extra := s.quiet(t, "reload", 600*time.Millisecond); extra != 0 {
		t.Errorf("got %d further reloads, want the writes debounced into one", extra)
	}
}

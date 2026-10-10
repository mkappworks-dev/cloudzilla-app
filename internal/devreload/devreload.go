// Package devreload refreshes a developer's browser when the dev server
// restarts or an asset changes. It is wired up only by a dev build, but carries
// no build tag itself so that it can be tested.
package devreload

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// debounce coalesces the several filesystem events one Tailwind build emits.
const debounce = 100 * time.Millisecond

// keepalive keeps intermediaries from closing an idle stream.
const keepalive = 20 * time.Second

// retryAfter is how long the browser waits before reconnecting a dropped
// stream, and so most of the delay between the new server listening and the
// page refreshing. EventSource defaults to several seconds.
const retryAfter = 500 * time.Millisecond

// Hub broadcasts reload events to the browsers watching this server.
type Hub struct {
	instanceID string
	closed     chan struct{}
	closeOnce  sync.Once

	mu   sync.Mutex
	subs map[chan struct{}]struct{}
}

func New(instanceID string) *Hub {
	return &Hub{
		instanceID: instanceID,
		closed:     make(chan struct{}),
		subs:       map[chan struct{}]struct{}{},
	}
}

// Close ends every open stream. A stream never finishes by itself, and
// http.Server.Shutdown waits for in-flight requests, so without this an open
// tab holds each restart open until the reloader's kill_delay expires.
func (h *Hub) Close() {
	h.closeOnce.Do(func() { close(h.closed) })
}

// Events streams reload events. The hello event carries this server's instance
// id: the client reloads when the id it sees differs from the last one, so a
// restart refreshes the page exactly once and a refresh cannot arrive before
// the server that served it.
func (h *Hub) Events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	head := w.Header()
	head.Set("Content-Type", "text/event-stream")
	head.Set("Cache-Control", "no-store")
	head.Set("Connection", "keep-alive")

	ch := h.subscribe()
	defer h.unsubscribe(ch)

	// A write that fails means the tab is gone, so stop rather than stream on.
	send := func(format string, args ...any) bool {
		if _, err := fmt.Fprintf(w, format, args...); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	if !send("retry: %d\n\nevent: hello\ndata: %s\n\n",
		retryAfter.Milliseconds(), h.instanceID) {
		return
	}

	ticker := time.NewTicker(keepalive)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-h.closed:
			return
		case <-ch:
			if !send("event: reload\ndata: 1\n\n") {
				return
			}
		case <-ticker.C:
			if !send("event: ping\ndata: 1\n\n") {
				return
			}
		}
	}
}

// Reload asks every browser to refresh.
func (h *Hub) Reload() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		// Each subscriber holds one slot, so a tab that has stopped reading
		// neither blocks this loop nor queues a refresh per event.
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Watch broadcasts a reload when anything under dir changes, which is what
// makes a Tailwind or JS edit visible without rebuilding the server. It returns
// when stop is closed; a nil stop means run for the life of the process.
func (h *Hub) Watch(stop <-chan struct{}, dir string) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer func() { _ = watcher.Close() }()

	// fsnotify does not recurse, so every directory is added by hand.
	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case !d.IsDir():
			return nil
		}
		return watcher.Add(path)
	})
	if err != nil {
		return err
	}

	var pending *time.Timer
	fired := make(chan struct{}, 1)
	for {
		select {
		case <-stop:
			return nil
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) == 0 {
				continue
			}
			if event.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					_ = watcher.Add(event.Name)
				}
			}
			if pending != nil {
				pending.Stop()
			}
			pending = time.AfterFunc(debounce, func() {
				select {
				case fired <- struct{}{}:
				default:
				}
			})
		case <-fired:
			h.Reload()
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			return err
		}
	}
}

func (h *Hub) subscribe() chan struct{} {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.subs[ch] = struct{}{}
	return ch
}

func (h *Hub) unsubscribe(ch chan struct{}) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subs, ch)
}

// Script serves the client. It compares instance ids rather than counting
// connections: after a reload the page reconnects to the same server, sees the
// same id and stops, so a reload cannot loop.
func (h *Hub) Script(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(clientJS))
}

const clientJS = `(function () {
	var KEY = 'cz-dev-instance';
	var source = new EventSource('/_dev/reload');
	source.addEventListener('hello', function (event) {
		var previous = null;
		try {
			previous = sessionStorage.getItem(KEY);
			sessionStorage.setItem(KEY, event.data);
		} catch (e) {
			return;
		}
		if (previous && previous !== event.data) {
			location.reload();
		}
	});
	source.addEventListener('reload', function () {
		location.reload();
	});
})();
`

// InstanceID is a value that changes every time the process starts.
func InstanceID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

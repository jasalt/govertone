package nrepl_test

import (
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/example/letgo-sointu/internal/analysis"
	"github.com/example/letgo-sointu/internal/app"
	"github.com/example/letgo-sointu/internal/audio"
	"github.com/example/letgo-sointu/internal/clock"
	musicnrepl "github.com/example/letgo-sointu/internal/nrepl"
	"github.com/zeebo/bencode"
)

type client struct {
	conn net.Conn
	dec  *bencode.Decoder
}

func startServer(t *testing.T, maxOutput int) (*app.App, *musicnrepl.Server) {
	t.Helper()
	a, err := app.New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	portFile := t.TempDir() + "/.nrepl-port"
	server := musicnrepl.New(a.Lisp, musicnrepl.Config{Port: 0, PortFile: portFile, WritePortFile: true, MaxOutputBytes: maxOutput})
	if err = server.Start(); err != nil {
		a.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = server.Stop()
		a.Close()
	})
	data, err := os.ReadFile(portFile)
	if err != nil {
		t.Fatalf("read port file: %v", err)
	}
	if got, _ := strconv.Atoi(string(data)); got != server.Port() || got == 0 {
		t.Fatalf("port file %q, bound port %d", data, server.Port())
	}
	return a, server
}

func dial(t *testing.T, server *musicnrepl.Server) *client {
	t.Helper()
	conn, err := net.DialTimeout("tcp", server.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &client{conn: conn, dec: bencode.NewDecoder(conn)}
}

func (c *client) request(t *testing.T, request map[string]any) []map[string]any {
	t.Helper()
	encoded, err := bencode.EncodeBytes(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.conn.Write(encoded); err != nil {
		t.Fatal(err)
	}
	var responses []map[string]any
	for {
		var response map[string]any
		if err = c.dec.Decode(&response); err != nil {
			t.Fatal(err)
		}
		responses = append(responses, response)
		if hasStatus(response, "done") {
			return responses
		}
	}
}

func hasStatus(response map[string]any, wanted string) bool {
	switch statuses := response["status"].(type) {
	case []any:
		for _, status := range statuses {
			if status == wanted {
				return true
			}
		}
	case []string:
		for _, status := range statuses {
			if status == wanted {
				return true
			}
		}
	}
	return false
}

func responseField(responses []map[string]any, field string) string {
	for _, response := range responses {
		if value, ok := response[field].(string); ok {
			return value
		}
	}
	return ""
}

func TestEvalSessionNamespaceAndMusicAPI(t *testing.T) {
	a, server := startServer(t, 0)
	c := dial(t, server)
	clone := c.request(t, map[string]any{"op": "clone", "id": "clone-1"})
	sessionID := responseField(clone, "new-session")
	if sessionID == "" {
		t.Fatalf("clone response: %#v", clone)
	}

	responses := c.request(t, map[string]any{"op": "eval", "id": "eval-1", "session": sessionID, "code": "(+ 1 2)"})
	if value := responseField(responses, "value"); value != "3" {
		t.Fatalf("value %q in %#v", value, responses)
	}
	responses = c.request(t, map[string]any{"op": "eval", "id": "eval-2", "session": sessionID, "code": "(ns editor.one)\n(def answer 41)"})
	if ns := responseField(responses, "ns"); ns != "editor.one" {
		t.Fatalf("namespace %q in %#v", ns, responses)
	}
	responses = c.request(t, map[string]any{"op": "eval", "id": "eval-3", "session": sessionID, "code": "(+ answer 1)"})
	if value := responseField(responses, "value"); value != "42" {
		t.Fatalf("session namespace did not persist: %#v", responses)
	}

	definition := `(defsynth remote-tone {:voices 2}
	  (oscillator {:type :sine})
	  (out {:gain 72}))`
	responses = c.request(t, map[string]any{"op": "load-file", "id": "load-1", "session": sessionID, "file": "(in-ns 'music.core)\n" + definition})
	if errText := responseField(responses, "err"); errText != "" {
		t.Fatalf("remote defsynth: %s", errText)
	}
	found := false
	for _, definition := range a.PatchRegistry.Snapshot().Definitions {
		found = found || definition.ID == "remote-tone"
	}
	if !found {
		t.Fatal("nREPL evaluation did not reach process-global music API")
	}
}

func TestNREPLPlayRendersAudio(t *testing.T) {
	a, server := startServer(t, 0)
	c := dial(t, server)
	responses := c.request(t, map[string]any{"op": "eval", "id": "play", "code": `(play :sine :a4 {:at 0 :dur 1})`})
	if errText := responseField(responses, "err"); errText != "" {
		t.Fatalf("remote play: %s", errText)
	}
	frames := clock.SampleRate * 2
	samples, err := audio.RenderOffline(a.Engine, frames, 256)
	if err != nil {
		t.Fatal(err)
	}
	report, err := analysis.Analyze(&analysis.WAV{SampleRate: clock.SampleRate, Channels: 2, Format: 3, Bits: 32, Samples: samples})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Finite || report.Left.Peak < 0.005 || report.DominantFrequencyHz < 439 || report.DominantFrequencyHz > 441 {
		t.Fatalf("unhealthy nREPL-triggered audio: %+v", report)
	}
	if output := os.Getenv("NREPL_AUDIO_OUTPUT"); output != "" {
		if err = audio.WriteWAV(output, samples); err != nil {
			t.Fatalf("write validation audio: %v", err)
		}
	}
}

func TestStructuredErrorsOutputBoundsAndInterrupt(t *testing.T) {
	_, server := startServer(t, 8)
	c := dial(t, server)

	responses := c.request(t, map[string]any{"op": "eval", "id": "out", "code": `(println "0123456789")`})
	if out := responseField(responses, "out"); len(out) != 8 {
		t.Fatalf("bounded output %q (%d bytes)", out, len(out))
	}
	foundOverflow := false
	for _, response := range responses {
		foundOverflow = foundOverflow || hasStatus(response, "nrepl-output-overflow")
	}
	if !foundOverflow {
		t.Fatalf("missing overflow status: %#v", responses)
	}

	responses = c.request(t, map[string]any{"op": "eval", "id": "bad", "code": "(unknown-function)"})
	if !strings.Contains(responseField(responses, "err"), "unknown-function") || responseField(responses, "ex") == "" {
		t.Fatalf("unstructured eval error: %#v", responses)
	}
	responses = c.request(t, map[string]any{"op": "interrupt", "id": "interrupt-1"})
	if !hasStatus(responses[len(responses)-1], "session-idle") {
		t.Fatalf("interrupt response: %#v", responses)
	}
	responses = c.request(t, map[string]any{"op": "eval", "id": "healthy", "code": "(+ 20 22)"})
	if responseField(responses, "value") != "42" {
		t.Fatalf("evaluation unhealthy after interrupt: %#v", responses)
	}
}

func TestSessionIsolationAndStopRemovesPortFile(t *testing.T) {
	_, server := startServer(t, 0)
	c := dial(t, server)
	first := responseField(c.request(t, map[string]any{"op": "clone", "id": "c1"}), "new-session")
	second := responseField(c.request(t, map[string]any{"op": "clone", "id": "c2"}), "new-session")
	c.request(t, map[string]any{"op": "eval", "id": "set", "session": first, "code": "(ns isolated.one)\n(def local-value 7)"})
	responses := c.request(t, map[string]any{"op": "eval", "id": "get", "session": second, "code": "local-value"})
	if responseField(responses, "err") == "" {
		t.Fatalf("session leaked namespace state: %#v", responses)
	}
	addr := server.Addr().String()
	if err := server.Stop(); err != nil {
		t.Fatal(err)
	}
	if conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Fatal("server still accepted connections after Stop")
	}
}

package nrepl_test

import (
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
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

	definition := `(defsynth remote-tone {:voices 2 :params {:level {:default 32 :min 0 :max 128}}}
	  (oscillator {:type :sine})
	  (out {:gain (param :level)}))`
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
	responses = c.request(t, map[string]any{"op": "eval", "id": "ctl-1", "session": sessionID, "code": `(ctl :remote-tone :level 90)`})
	if errText := responseField(responses, "err"); errText != "" {
		t.Fatalf("remote ctl: %s", errText)
	}
	if _, renderErr := audio.RenderOffline(a.Engine, 1, 1); renderErr != nil {
		t.Fatal(renderErr)
	}
	trace := a.Engine.ControlTrace()
	if len(trace.Events) != 1 || trace.Events[0].Parameter != "level" || trace.Events[0].Value != 90 {
		t.Fatalf("remote ctl did not reach scheduler: %#v", trace.Events)
	}
	responses = c.request(t, map[string]any{"op": "eval", "id": "ramp-1", "session": sessionID, "code": `(ramp :remote-tone :level 90 40 {:dur 1 :curve :linear})`})
	if errText := responseField(responses, "err"); errText != "" {
		t.Fatalf("remote ramp: %s", errText)
	}
	if _, renderErr := audio.RenderOffline(a.Engine, 22051, 256); renderErr != nil {
		t.Fatal(renderErr)
	}
	automationTrace := a.Engine.AutomationTrace()
	if len(automationTrace.Events) != 2 || automationTrace.Events[0].Kind != "start" || automationTrace.Events[1].Kind != "complete" {
		t.Fatalf("remote ramp did not reach automation evaluator: %#v", automationTrace.Events)
	}
}

func TestBreplStyleLoadFileEval(t *testing.T) {
	_, server := startServer(t, 0)
	c := dial(t, server)
	path := t.TempDir() + "/brepl-file.lg"
	source := "(in-ns 'music.core)\n(def brepl-file-value 40)\n(+ brepl-file-value 2)\n"
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	responses := c.request(t, map[string]any{"op": "eval", "id": "brepl-load-file", "code": `(load-file "` + path + `")`})
	if errText := responseField(responses, "err"); errText != "" {
		t.Fatalf("brepl-style load-file: %s", errText)
	}
	if value := responseField(responses, "value"); value != "42" {
		t.Fatalf("load-file value %q in %#v", value, responses)
	}
}

func TestNREPLPlayRendersAudio(t *testing.T) {
	a, server := startServer(t, 0)
	c := dial(t, server)
	responses := c.request(t, map[string]any{"op": "eval", "id": "play", "code": `(play :sine :a4 {:at 0 :dur 1})`})
	if errText := responseField(responses, "err"); errText != "" {
		t.Fatalf("remote play: %s", errText)
	}
	responses = c.request(t, map[string]any{"op": "interrupt", "id": "interrupt-audio"})
	if !hasStatus(responses[len(responses)-1], "session-idle") {
		t.Fatalf("interrupt response: %#v", responses)
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

func TestSlowAndMalformedClientsDoNotBlockEvaluation(t *testing.T) {
	_, server := startServer(t, 0)
	slow, err := net.DialTimeout("tcp", server.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer slow.Close()
	// Leave one decoder waiting forever on an incomplete bencode value.
	if _, err = slow.Write([]byte("d4:code100:")); err != nil {
		t.Fatal(err)
	}

	healthy := dial(t, server)
	responses := healthy.request(t, map[string]any{"op": "eval", "id": "while-slow", "code": "(+ 40 2)"})
	if responseField(responses, "value") != "42" {
		t.Fatalf("slow peer blocked healthy eval: %#v", responses)
	}

	malformed, err := net.DialTimeout("tcp", server.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = malformed.Write([]byte("not-bencode"))
	_ = malformed.Close()
	responses = healthy.request(t, map[string]any{"op": "describe", "id": "after-malformed"})
	if !hasStatus(responses[len(responses)-1], "done") {
		t.Fatalf("malformed peer damaged server: %#v", responses)
	}
}

func TestTerminalAndNREPLEvaluationShareSerializedBoundary(t *testing.T) {
	a, server := startServer(t, 0)
	var wg sync.WaitGroup
	errors := make(chan string, 40)
	for index := 0; index < 20; index++ {
		wg.Add(2)
		go func(id int) {
			defer wg.Done()
			peer := dial(t, server)
			responses := peer.request(t, map[string]any{"op": "eval", "id": strconv.Itoa(id), "code": "(+ 1 2)"})
			if responseField(responses, "value") != "3" {
				errors <- "nREPL evaluation failed"
			}
		}(index)
		go func() {
			defer wg.Done()
			value, err := a.Lisp.Eval("(+ 20 22)")
			if err != nil || value.String() != "42" {
				errors <- "terminal evaluation failed"
			}
		}()
	}
	wg.Wait()
	close(errors)
	for message := range errors {
		t.Error(message)
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

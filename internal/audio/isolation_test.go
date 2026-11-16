package audio

import (
	"os/exec"
	"strings"
	"testing"
)

// Phase 3 §43 "no audio-thread Lisp evaluation": the engine and scheduler
// cannot evaluate Lisp because they do not link the interpreter at all.
// Everything the audio thread runs is in these packages and their
// dependencies.
func TestAudioPathDoesNotLinkLisp(t *testing.T) {
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go tool not on PATH")
	}
	out, err := exec.Command(goTool, "list", "-deps", ".", "../scheduler").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	for _, dep := range strings.Fields(string(out)) {
		if strings.Contains(dep, "let-go") || strings.HasSuffix(dep, "/internal/lisp") || strings.HasSuffix(dep, "/internal/nrepl") {
			t.Fatalf("the audio path links %s", dep)
		}
	}
}

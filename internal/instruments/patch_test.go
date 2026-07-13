package instruments

import "testing"

func TestBuiltinPatchFingerprintAndLayout(t *testing.T) {
	p := BuiltinProvider{}
	const want = "sha256:eb1012d46e956dcda3ddaffd748f9577f01e625e1131b2d2f04b77df60eed24a"
	if got := p.Fingerprint(); got != want {
		t.Fatalf("built-in patch changed: got %s, want %s; calibrate fixtures before updating", got, want)
	}
	patch := p.Patch()
	if patch.NumVoices() != 24 {
		t.Fatalf("got %d voices", patch.NumVoices())
	}
}

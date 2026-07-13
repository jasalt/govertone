package instruments

import "testing"

func TestBuiltinPatchFingerprintAndLayout(t *testing.T) {
	p := BuiltinProvider{}
	const want = "sha256:b1519e7eb1a127ba029083a3e582adc3c0532d71263e5003ecf2099051fb484f"
	if got := p.Fingerprint(); got != want {
		t.Fatalf("built-in patch changed: got %s, want %s; calibrate fixtures before updating", got, want)
	}
	patch := p.Patch()
	if patch.NumVoices() != 24 {
		t.Fatalf("got %d voices", patch.NumVoices())
	}
}

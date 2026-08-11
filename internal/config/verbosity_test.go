package config

import "testing"

func TestDefaultVerbosity(t *testing.T) {
	d := DefaultVerbosity()
	if !d[ModelDeepSeekV4Flash] {
		t.Fatalf("flash should default to verbosity on, got %v", d)
	}
	if d[ModelDeepSeekV4Pro] {
		t.Fatalf("pro should default to verbosity off, got %v", d)
	}
	if len(d) != len(VerbosityCatalog()) {
		t.Fatalf("default map has %d models, catalog has %d", len(d), len(VerbosityCatalog()))
	}
}

func TestNormalizeVerbosityMap(t *testing.T) {
	// nil fills all defaults.
	got := NormalizeVerbosityMap(nil)
	if !got[ModelDeepSeekV4Flash] {
		t.Fatal("nil map should default flash to on")
	}

	// Explicit flash-off is honored, pro unchanged.
	in := map[string]bool{ModelDeepSeekV4Flash: false}
	got = NormalizeVerbosityMap(in)
	if got[ModelDeepSeekV4Flash] {
		t.Fatal("flash should be off")
	}
	if got[ModelDeepSeekV4Pro] {
		t.Fatal("pro should remain default off")
	}

	// Unknown model keys are dropped.
	got = NormalizeVerbosityMap(map[string]bool{"bogus-model": true})
	if _, ok := got["bogus-model"]; ok {
		t.Fatal("unknown model key should be dropped")
	}
}

func TestVerbosityFor(t *testing.T) {
	if !VerbosityFor(map[string]bool{ModelDeepSeekV4Flash: true}, ModelDeepSeekV4Flash) {
		t.Fatal("flash verbosity should be on")
	}
	if VerbosityFor(map[string]bool{ModelDeepSeekV4Flash: false}, ModelDeepSeekV4Flash) {
		t.Fatal("flash verbosity should be off")
	}
	// Unknown model returns false.
	if VerbosityFor(nil, "glm-4.7") {
		t.Fatal("unknown model should not have verbosity")
	}
}

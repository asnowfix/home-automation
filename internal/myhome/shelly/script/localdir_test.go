package script

import (
	"crypto/sha1"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/asnowfix/home-automation/internal/shelly/scripts"

	"github.com/go-logr/logr/testr"
)

// embeddedScriptName is a real embedded script used as a fixture across these
// tests. Any name from internal/shelly/scripts/*.js would do; watchdog.js is
// small and already relied upon elsewhere (see setup.SetupDevice).
const embeddedScriptName = "watchdog.js"

func TestLoadScript_FallbackToEmbedded_WhenDirEmpty(t *testing.T) {
	log := testr.New(t)

	// dir == "" must disable local loading outright, per issue #457's
	// "fallback is silent and total" requirement: an empty/unset directory
	// changes nothing versus today's embedded-only behavior.
	code, source, err := LoadScript(log, "", embeddedScriptName)
	if err != nil {
		t.Fatalf("LoadScript returned error: %v", err)
	}
	if source != "embedded" {
		t.Errorf("source = %q, want %q", source, "embedded")
	}

	want, err := fs.ReadFile(scripts.GetFS(), embeddedScriptName)
	if err != nil {
		t.Fatalf("failed to read embedded fixture: %v", err)
	}
	if string(code) != string(want) {
		t.Errorf("code does not match embedded content")
	}
}

func TestLoadScript_FallbackToEmbedded_WhenDirDoesNotContainName(t *testing.T) {
	log := testr.New(t)
	dir := t.TempDir() // empty directory: the name resolution requirement

	code, source, err := LoadScript(log, dir, embeddedScriptName)
	if err != nil {
		t.Fatalf("LoadScript returned error: %v", err)
	}
	if source != "embedded" {
		t.Errorf("source = %q, want %q", source, "embedded")
	}

	want, err := fs.ReadFile(scripts.GetFS(), embeddedScriptName)
	if err != nil {
		t.Fatalf("failed to read embedded fixture: %v", err)
	}
	if string(code) != string(want) {
		t.Errorf("code does not match embedded content")
	}
}

func TestLoadScript_LocalPrecedence(t *testing.T) {
	log := testr.New(t)
	dir := t.TempDir()

	localContent := []byte("// locally edited variant, not what is embedded\n")
	if err := os.WriteFile(filepath.Join(dir, embeddedScriptName), localContent, 0o644); err != nil {
		t.Fatalf("failed to write local fixture: %v", err)
	}

	code, source, err := LoadScript(log, dir, embeddedScriptName)
	if err != nil {
		t.Fatalf("LoadScript returned error: %v", err)
	}
	if source != "local" {
		t.Errorf("source = %q, want %q", source, "local")
	}
	if string(code) != string(localContent) {
		t.Errorf("code = %q, want local content %q", code, localContent)
	}
}

// TestLoadScript_LocalHashDiffersFromEmbedded proves the version-tracking
// claim in issue #457: UploadWithVersion hashes whatever []byte it is given
// (see UploadWithVersion in ops.go), so a locally-loaded script that differs
// from the embedded one simply produces its own SHA1 and its own KVS marker
// — no special-casing required, and nothing to break.
func TestLoadScript_LocalHashDiffersFromEmbedded(t *testing.T) {
	log := testr.New(t)
	dir := t.TempDir()

	localContent := []byte("// experiment: delete a per-call allocation\n")
	if err := os.WriteFile(filepath.Join(dir, embeddedScriptName), localContent, 0o644); err != nil {
		t.Fatalf("failed to write local fixture: %v", err)
	}

	localCode, localSource, err := LoadScript(log, dir, embeddedScriptName)
	if err != nil {
		t.Fatalf("LoadScript(local) returned error: %v", err)
	}
	if localSource != "local" {
		t.Fatalf("expected local source, got %q", localSource)
	}

	embeddedCode, embeddedSource, err := LoadScript(log, "", embeddedScriptName)
	if err != nil {
		t.Fatalf("LoadScript(embedded) returned error: %v", err)
	}
	if embeddedSource != "embedded" {
		t.Fatalf("expected embedded source, got %q", embeddedSource)
	}

	localHash := sha1.Sum(localCode)
	embeddedHash := sha1.Sum(embeddedCode)
	if localHash == embeddedHash {
		t.Fatalf("expected local and embedded content to hash differently (local content was deliberately changed)")
	}
}

func TestLoadScript_RejectsSubdirectory(t *testing.T) {
	log := testr.New(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "x.js"), []byte("//x"), 0o644); err != nil {
		t.Fatalf("failed to write nested fixture: %v", err)
	}

	_, _, err := LoadScript(log, dir, "sub/x.js")
	if err == nil {
		t.Fatal("expected an error for a name containing a path separator, got nil")
	}
}

func TestLoadScript_RejectsPathTraversal(t *testing.T) {
	log := testr.New(t)
	dir := t.TempDir()

	_, _, err := LoadScript(log, dir, "../etc/passwd")
	if err == nil {
		t.Fatal("expected an error for a path-traversal name, got nil")
	}
}

func TestLoadScript_RejectsDotAndDotDot(t *testing.T) {
	log := testr.New(t)
	dir := t.TempDir()

	for _, name := range []string{".", ".."} {
		if _, _, err := LoadScript(log, dir, name); err == nil {
			t.Errorf("LoadScript(%q): expected error, got nil", name)
		}
	}
}

func TestLoadScript_UnknownNameErrors(t *testing.T) {
	log := testr.New(t)

	_, _, err := LoadScript(log, "", "definitely-not-a-real-script.js")
	if err == nil {
		t.Fatal("expected an error for a name present in neither source, got nil")
	}
}

// TestLoadScript_NoLocalScriptsDisablesEvenWhenFilePresent exercises the
// scenario the CLI wires up via --no-local-scripts: even when dir points at
// a directory that DOES contain a same-named file, passing dir == "" (what
// the CLI's localScriptsDirEffective() returns once --no-local-scripts is
// set) must resolve to the embedded copy, not the local one.
func TestLoadScript_NoLocalScriptsDisablesEvenWhenFilePresent(t *testing.T) {
	log := testr.New(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, embeddedScriptName), []byte("// stray local copy\n"), 0o644); err != nil {
		t.Fatalf("failed to write local fixture: %v", err)
	}

	// Simulate --no-local-scripts forcing the effective dir to "".
	code, source, err := LoadScript(log, "", embeddedScriptName)
	if err != nil {
		t.Fatalf("LoadScript returned error: %v", err)
	}
	if source != "embedded" {
		t.Errorf("source = %q, want %q (the local file must be ignored)", source, "embedded")
	}

	want, err := fs.ReadFile(scripts.GetFS(), embeddedScriptName)
	if err != nil {
		t.Fatalf("failed to read embedded fixture: %v", err)
	}
	if string(code) != string(want) {
		t.Errorf("code does not match embedded content")
	}
}

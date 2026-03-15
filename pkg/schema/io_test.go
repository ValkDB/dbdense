package schema

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeExportFile(t *testing.T, path string, export CtxExport) {
	t.Helper()
	data, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("marshal export: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write export: %v", err)
	}
}

func TestLoadExport_AcceptsCompatibleVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ctxexport.json")
	writeExportFile(t, path, CtxExport{
		Version: "ctxexport.v2",
		Entities: []Entity{
			{Name: "users", Fields: []Field{{Name: "id", IsPK: true}}},
		},
	})

	export, err := LoadExport(path)
	if err != nil {
		t.Fatalf("LoadExport error: %v", err)
	}
	if export.Version != "ctxexport.v2" {
		t.Fatalf("LoadExport version = %q, want %q", export.Version, "ctxexport.v2")
	}
}

func TestLoadExport_RejectsNonexistentFile(t *testing.T) {
	_, err := LoadExport(filepath.Join(t.TempDir(), "missing.json"))
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
	if !strings.Contains(err.Error(), "load export") {
		t.Errorf("expected 'load export' in error, got: %v", err)
	}
}

func TestLoadExport_RejectsInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := LoadExport(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("expected 'parse' in error, got: %v", err)
	}
}

// NOTE: LoadExport rejects files exceeding MaxExportFileSize (100 MB) via
// os.Stat before reading. This path is not tested because MaxExportFileSize
// is a const and creating a 100 MB temp file in unit tests is impractical.
// The guard is a simple info.Size() > MaxExportFileSize check in io.go.

func TestLoadExport_RejectsUnsupportedVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ctxexport.json")
	writeExportFile(t, path, CtxExport{
		Version: "v0",
		Entities: []Entity{
			{Name: "users", Fields: []Field{{Name: "id", IsPK: true}}},
		},
	})

	_, err := LoadExport(path)
	if err == nil {
		t.Fatal("expected error for unsupported version")
	}
	if !strings.Contains(err.Error(), `unsupported export version "v0"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

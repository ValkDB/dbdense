package schema

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MaxExportFileSize is the maximum allowed size for a ctxexport.json file.
const MaxExportFileSize = 100 << 20 // 100 MB

// LoadExport reads a ctxexport.json file from disk and decodes it.
func LoadExport(path string) (*CtxExport, error) {
	path = filepath.Clean(path)

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("load export %q: %w", path, err)
	}
	if info.Size() > MaxExportFileSize {
		return nil, fmt.Errorf("load export %q: file too large (%d bytes, max %d)", path, info.Size(), MaxExportFileSize)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load export %q: read: %w", path, err)
	}
	var export CtxExport
	if err := json.Unmarshal(data, &export); err != nil {
		return nil, fmt.Errorf("load export %q: parse: %w", path, err)
	}
	if !strings.HasPrefix(export.Version, "ctxexport.") {
		return nil, fmt.Errorf("load export %q: unsupported export version %q (expected ctxexport.v0 or compatible)", path, export.Version)
	}
	if err := export.ValidateStrict(); err != nil {
		return nil, fmt.Errorf("load export %q: %w", path, err)
	}
	return &export, nil
}

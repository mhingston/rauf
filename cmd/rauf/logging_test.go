package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteLogEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}

	entry := logEntry{
		Type:      "test",
		Iteration: 1,
		Mode:      "build",
	}

	writeLogEntry(file, entry)
	file.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var decoded logEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Type != "test" || decoded.Iteration != 1 || decoded.Mode != "build" {
		t.Errorf("unexpected decoded content: %+v", decoded)
	}
}

func TestWriteLogEntry_Nil(t *testing.T) {
	// Should not panic
	writeLogEntry(nil, logEntry{Type: "nil"})
}

func TestOpenLogFile_MkdirFail(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	if err := os.WriteFile(logDir, []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("write log file placeholder: %v", err)
	}

	_, _, err := openLogFile("test", logDir)
	if err == nil {
		t.Errorf("expected error when mkdir fails")
	}
}

package console

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	config "github.com/theapemachine/six/pkg/system/core"
)

func TestNew(t *testing.T) {
	t.Run("returns Logger with non-nil file when ProjectRoot is valid", func(t *testing.T) {
		tempDir := t.TempDir()
		bak := config.System.ProjectRoot
		t.Cleanup(func() { config.System.ProjectRoot = bak })
		config.System.ProjectRoot = tempDir

		l, err := New()
		if err != nil {
			t.Fatalf("New() failed: %v", err)
		}
		if l == nil {
			t.Fatal("New() returned nil Logger")
		}
		if l.file == nil {
			t.Fatal("Logger.file is nil")
		}
		_ = l.Close()
	})

	t.Run("returns error when file open fails", func(t *testing.T) {
		bak := config.System.ProjectRoot
		t.Cleanup(func() { config.System.ProjectRoot = bak })
		config.System.ProjectRoot = filepath.Join(os.TempDir(), "nonexistent_xyz_12345")

		_, err := New()
		if err == nil {
			t.Fatal("New() should fail when ProjectRoot path is invalid")
		}
	})
}

func TestLoggerPackageLevel(t *testing.T) {
	t.Run("logger is set after init", func(t *testing.T) {
		if logger == nil {
			t.Fatal("package-level logger is nil")
		}
	})
}

func TestLoggerHelpers(t *testing.T) {
	t.Run("Info does not panic", func(t *testing.T) {
		Info("test info")
	})
	t.Run("Trace does not panic", func(t *testing.T) {
		Trace("test trace")
	})
	t.Run("Error returns nil for nil input", func(t *testing.T) {
		if err := Error(nil); err != nil {
			t.Fatalf("Error(nil) should return nil, got %v", err)
		}
	})
	t.Run("Error does not panic for non-nil", func(t *testing.T) {
		e := fmt.Errorf("test error")
		if err := Error(e); err != e {
			t.Fatalf("Error should return same error, got %v", err)
		}
	})
	t.Run("Warn does not panic", func(t *testing.T) {
		Warn(stringer("test warn"))
	})
	t.Run("Debug does not panic", func(t *testing.T) {
		Debug("test debug")
	})
	t.Run("Close does not panic", func(t *testing.T) {
		l, err := New()
		if err != nil {
			t.Skipf("New failed, cannot test Close: %v", err)
		}
		if err := l.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})
	t.Run("SetLevel does not panic", func(t *testing.T) {
		SetLevel(0)
	})
}

type stringer string

func (s stringer) String() string { return string(s) }

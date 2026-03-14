package utils

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestFilesystemFunctions(t *testing.T) {
	Convey("Given filesystem utilities", t, func() {
		Convey("ProjectRoot", func() {
			Convey("It should traverse upwards to find go.mod", func() {
				// Setup dummy structure
				tempDir := t.TempDir()
				err := os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte("module test"), 0644)
				So(err, ShouldBeNil)

				nestedDir := filepath.Join(tempDir, "a", "b", "c")
				err = os.MkdirAll(nestedDir, 0755)
				So(err, ShouldBeNil)

				originalWd, err := os.Getwd()
				So(err, ShouldBeNil)
				t.Cleanup(func() { _ = os.Chdir(originalWd) })
				err = os.Chdir(nestedDir)
				So(err, ShouldBeNil)

				root := ProjectRoot()
				canonical, err := filepath.EvalSymlinks(tempDir)
				So(err, ShouldBeNil)
				So(root, ShouldEqual, canonical)
			})
		})

		Convey("CheckFileExists", func() {
			Convey("It should return true for an existing file and false for non-existent path", func() {
				tempDir := t.TempDir()
				file := filepath.Join(tempDir, "test.txt")

				So(CheckFileExists(file), ShouldBeFalse)

				err := os.WriteFile(file, []byte("content"), 0644)
				So(err, ShouldBeNil)
				So(CheckFileExists(file), ShouldBeTrue)
			})
		})
	})
}

func BenchmarkProjectRoot(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = ProjectRoot()
	}
}

func BenchmarkCheckFileExists(b *testing.B) {
	tempDir := b.TempDir()
	file := filepath.Join(tempDir, "bench.txt")
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		b.Fatalf("setup: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CheckFileExists(file)
	}
}

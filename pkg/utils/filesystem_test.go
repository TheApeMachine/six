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

				// switch working directory
				originalWd, _ := os.Getwd()
				t.Cleanup(func() { os.Chdir(originalWd) })
				os.Chdir(nestedDir)

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

				os.WriteFile(file, []byte("content"), 0644)
				So(CheckFileExists(file), ShouldBeTrue)
			})
		})
	})
}

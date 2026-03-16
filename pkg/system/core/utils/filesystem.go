package utils

import (
	"os"
	"path/filepath"
)

/*
ProjectRoot walks upward from the current directory until it finds go.mod.
Returns that directory, or "." if not found.
*/
func ProjectRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}

	for dir := wd; dir != ""; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		if dir == filepath.Dir(dir) {
			break
		}
	}
	return "."
}

/*
CheckFileExists returns true if path exists and is accessible; false if not found or inaccessible.
*/
func CheckFileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}



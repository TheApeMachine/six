package utils

import (
	"os"
	"path/filepath"
)

// ProjectRoot returns the directory containing go.mod, or "." if not found.
func ProjectRoot() string {
	wd, _ := os.Getwd()
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

func CheckFileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

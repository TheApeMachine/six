package errnie

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/viper"
)

func createTempDir(t *testing.T) string {
	dir, err := os.MkdirTemp("", "logger_test_*")
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLogger(t *testing.T) {
	Convey("Given a logger instance", t, func() {
		originalWd, _ := os.Getwd()
		originalGoroutines := runtime.NumGoroutine()

		Reset(func() {
			os.Unsetenv("LOGFILE")
			os.Unsetenv("NOCONSOLE")
			os.Unsetenv("LOGGOROUTINES")
			os.Unsetenv("NOSTACK")
			os.Unsetenv("NOSNIPPET")

			if logFile != nil {
				logFile.Close()
				logFile = nil
			}

			os.Chdir(originalWd)
			viper.Reset()
		})

		Convey("When initializing the logger", func() {
			Convey("It should initialize without errors when LOGFILE is not set", func() {
				os.Setenv("LOGFILE", "false")
				InitLogger()
				So(logFile, ShouldBeNil)
			})

			Convey("It should create a log file when LOGFILE is set to true", func() {
				tmpDir := createTempDir(t)
				defer os.RemoveAll(tmpDir)
				os.Chdir(tmpDir)

				os.Setenv("LOGFILE", "true")
				InitLogger()
				So(logFile, ShouldNotBeNil)
			})

			Convey("It should handle goroutine logging when enabled", func() {
				tmpDir := createTempDir(t)
				defer os.RemoveAll(tmpDir)
				os.Chdir(tmpDir)

				os.Setenv("LOGFILE", "true")
				os.Setenv("LOGGOROUTINES", "true")
				viper.Set("loglevel", "debug")

				InitLogger()

				// Verify log file was created
				So(logFile, ShouldNotBeNil)

				// Wait a bit for the goroutine to start
				time.Sleep(100 * time.Millisecond)

				// Verify that a new goroutine was created
				currentGoroutines := runtime.NumGoroutine()
				So(currentGoroutines, ShouldBeGreaterThan, originalGoroutines)

				// Now verify that it's actually logging
				Debug("trigger log sync")
				time.Sleep(100 * time.Millisecond)

				content, err := os.ReadFile(filepath.Join("logs", "amsh.log"))
				So(err, ShouldBeNil)
				So(string(content), ShouldContainSubstring, "trigger log sync")
			})
		})

		Convey("When using trace logging", func() {
			tmpDir := createTempDir(t)
			defer os.RemoveAll(tmpDir)
			os.Chdir(tmpDir)
			
			Convey("It should handle trace with console output", func() {
				os.Setenv("LOGFILE", "true")
				os.Setenv("NOCONSOLE", "false")
				viper.Set("loglevel", "debug")
				InitLogger()
				
				Trace("trace message", "key", "value", 42)
				time.Sleep(100 * time.Millisecond)
				
				content, err := os.ReadFile(filepath.Join("logs", "amsh.log"))
				So(err, ShouldBeNil)
				So(string(content), ShouldContainSubstring, "trace message")
				So(string(content), ShouldContainSubstring, "key")
				So(string(content), ShouldContainSubstring, "value")
				So(string(content), ShouldContainSubstring, "42")
			})
			
			Convey("It should handle trace with console disabled", func() {
				os.Setenv("LOGFILE", "true")
				os.Setenv("NOCONSOLE", "true")
				viper.Set("loglevel", "debug")
				InitLogger()
				
				Trace("trace message", "key", "value")
				time.Sleep(100 * time.Millisecond)
				
				content, err := os.ReadFile(filepath.Join("logs", "amsh.log"))
				So(err, ShouldBeNil)
				So(string(content), ShouldContainSubstring, "trace message")
			})
		})

		Convey("When setting log levels", func() {
			testCases := []struct {
				level    string
				expected string
			}{
				{"debug", "debug"},
				{"info", "info"},
				{"warn", "warn"},
				{"error", "error"},
				{"trace", "debug"},
				{"invalid", "debug"},
			}

			for _, tc := range testCases {
				viper.Set("loglevel", tc.level)
				setLogLevel()
				So(logger.GetLevel().String(), ShouldEqual, tc.expected)
			}
		})

		Convey("When logging messages", func() {
			Convey("It should write messages to log file when enabled", func() {
				tmpDir := createTempDir(t)
				defer os.RemoveAll(tmpDir)
				os.Chdir(tmpDir)

				os.Setenv("LOGFILE", "true")
				InitLogger()

				Log("%s", "test log message")
				time.Sleep(100 * time.Millisecond)

				content, err := os.ReadFile(filepath.Join("logs", "amsh.log"))
				So(err, ShouldBeNil)
				So(string(content), ShouldContainSubstring, "test log message")
			})

			Convey("Raw logging should handle various types of input", func() {
				tmpDir := createTempDir(t)
				defer os.RemoveAll(tmpDir)
				os.Chdir(tmpDir)

				os.Setenv("LOGFILE", "true")
				os.Setenv("NOCONSOLE", "true")
				InitLogger()

				testStruct := struct {
					Name string
					Age  int
				}{"Test", 42}

				Raw(testStruct)
				time.Sleep(100 * time.Millisecond)

				content, err := os.ReadFile(filepath.Join("logs", "amsh.log"))
				So(err, ShouldBeNil)
				So(string(content), ShouldContainSubstring, "Test")
				So(string(content), ShouldContainSubstring, "42")
			})
		})

		Convey("When handling errors", func() {
			Convey("Error function should return formatted error with stack trace", func() {
				testErr := errors.New("test error")
				resultErr := Error(testErr)

				So(resultErr, ShouldNotBeNil)
				So(resultErr.Error(), ShouldContainSubstring, "test error")
				So(resultErr.Error(), ShouldContainSubstring, "STACK TRACE")
			})

			Convey("Error function should return nil for nil error", func() {
				resultErr := Error(nil)
				So(resultErr, ShouldBeNil)
			})
		})

		Convey("When using different log levels", func() {
			tmpDir := createTempDir(t)
			defer os.RemoveAll(tmpDir)
			os.Chdir(tmpDir)

			os.Setenv("LOGFILE", "true")
			InitLogger()

			Convey("Debug should log messages at debug level", func() {
				Debug("debug message %s", "test")
				time.Sleep(100 * time.Millisecond)
				content, _ := os.ReadFile(filepath.Join("logs", "amsh.log"))
				So(string(content), ShouldContainSubstring, "debug message test")
			})

			Convey("Info should log messages at info level", func() {
				Info("info message %s", "test")
				time.Sleep(100 * time.Millisecond)
				content, _ := os.ReadFile(filepath.Join("logs", "amsh.log"))
				So(string(content), ShouldContainSubstring, "info message test")
			})

			Convey("Warn should log messages at warn level", func() {
				Warn("warn message %s", "test")
				time.Sleep(100 * time.Millisecond)
				content, _ := os.ReadFile(filepath.Join("logs", "amsh.log"))
				So(string(content), ShouldContainSubstring, "warn message test")
			})
		})

		Convey("When handling errors with code snippets", func() {
			tmpDir := createTempDir(t)
			defer os.RemoveAll(tmpDir)
			os.Chdir(tmpDir)
			
			// Create a test file with some content
			testFile := filepath.Join(tmpDir, "test.txt")
			content := []byte("line 1\nline 2\nline 3\nline 4\nline 5\n")
			err := os.WriteFile(testFile, content, 0644)
			So(err, ShouldBeNil)

			os.Setenv("LOGFILE", "true")
			InitLogger()

			Convey("It should include code snippets in error messages", func() {
				err := Error(errors.New(testFile))
				So(err, ShouldNotBeNil)
				errStr := err.Error()
				So(errStr, ShouldContainSubstring, "line 1")
				So(errStr, ShouldContainSubstring, "line 2")
				So(errStr, ShouldContainSubstring, "line 3")
			})

			Convey("It should handle missing files gracefully", func() {
				err := Error(errors.New("/nonexistent/file.txt"))
				So(err, ShouldNotBeNil)
				So(err.Error(), ShouldNotContainSubstring, "panic")
			})

			Convey("It should handle unreadable files gracefully", func() {
				// Create a file and make it unreadable
				unreadableFile := filepath.Join(tmpDir, "unreadable.txt")
				os.WriteFile(unreadableFile, []byte("test"), 0644)
				os.Chmod(unreadableFile, 0000)
				
				err := Error(errors.New(unreadableFile))
				So(err, ShouldNotBeNil)
				So(err.Error(), ShouldNotContainSubstring, "panic")
				
				// Cleanup
				os.Chmod(unreadableFile, 0644)
			})

			Convey("It should handle large files efficiently", func() {
				// Create a large file
				largeFile := filepath.Join(tmpDir, "large.txt")
				f, _ := os.Create(largeFile)
				for i := 1; i <= 1000; i++ {
					f.WriteString(fmt.Sprintf("line %d\n", i))
				}
				f.Close()

				err := Error(errors.New(largeFile))
				So(err, ShouldNotBeNil)
				errStr := err.Error()
				// Should contain lines around the beginning
				So(errStr, ShouldContainSubstring, "line 1")
				// Shouldn't contain all 1000 lines
				So(len(strings.Split(errStr, "\n")), ShouldBeLessThan, 100)
			})
		})

		Convey("When handling errors with different output configurations", func() {
			tmpDir := createTempDir(t)
			defer os.RemoveAll(tmpDir)
			os.Chdir(tmpDir)
			
			// Create a test file with some content
			testFile := filepath.Join(tmpDir, "test.txt")
			content := []byte("test content line 1\ntest content line 2\ntest content line 3\n")
			err := os.WriteFile(testFile, content, 0644)
			So(err, ShouldBeNil)

			os.Setenv("LOGFILE", "true")
			InitLogger()

			Convey("It should include both stack trace and code snippet by default", func() {
				err := Error(errors.New(testFile))
				errStr := err.Error()
				So(errStr, ShouldContainSubstring, "STACK TRACE")
				So(errStr, ShouldContainSubstring, "test content line")
			})

			Convey("It should exclude stack trace when NOSTACK is true", func() {
				os.Setenv("NOSTACK", "true")
				err := Error(errors.New(testFile))
				errStr := err.Error()
				So(errStr, ShouldNotContainSubstring, "STACK TRACE")
				So(errStr, ShouldNotContainSubstring, "test content line")
			})

			Convey("It should exclude code snippet when NOSNIPPET is true", func() {
				os.Setenv("NOSNIPPET", "true")
				err := Error(errors.New(testFile))
				errStr := err.Error()
				So(errStr, ShouldContainSubstring, "STACK TRACE")
				So(errStr, ShouldNotContainSubstring, "test content line")
			})

			Convey("It should exclude both when both flags are true", func() {
				os.Setenv("NOSTACK", "true")
				os.Setenv("NOSNIPPET", "true")
				err := Error(errors.New(testFile))
				errStr := err.Error()
				So(errStr, ShouldNotContainSubstring, "STACK TRACE")
				So(errStr, ShouldNotContainSubstring, "test content line")
				So(errStr, ShouldEqual, testFile)
			})
		})
	})
}

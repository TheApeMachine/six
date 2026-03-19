package errnie

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/log"
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

		Reset(func() {
			os.Unsetenv("LOGFILE")
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
			Convey("It should not create a log file when LOGFILE is not set", func() {
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
		})

		Convey("When using trace logging", func() {
			tmpDir := createTempDir(t)
			defer os.RemoveAll(tmpDir)
			os.Chdir(tmpDir)

			Convey("Trace should write to the log file", func() {
				os.Setenv("LOGFILE", "true")
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
			Convey("Error should return the original error unchanged", func() {
				testErr := errors.New("test error")
				resultErr := Error(testErr)

				So(resultErr, ShouldNotBeNil)
				So(resultErr, ShouldEqual, testErr)
				So(resultErr.Error(), ShouldEqual, "test error")
			})

			Convey("Error should return nil for nil error", func() {
				resultErr := Error(nil)
				So(resultErr, ShouldBeNil)
			})

			Convey("Error should preserve error chain for errors.Is", func() {
				sentinel := errors.New("sentinel")
				wrapped := fmt.Errorf("context: %w", sentinel)
				resultErr := Error(wrapped)

				So(errors.Is(resultErr, sentinel), ShouldBeTrue)
			})
		})

		Convey("When writing to log file", func() {
			tmpDir := createTempDir(t)
			defer os.RemoveAll(tmpDir)
			os.Chdir(tmpDir)

			os.Setenv("LOGFILE", "true")
			InitLogger()

			Convey("Debug should write keyvals to log file", func() {
				Debug("debug msg", "k", "v")
				time.Sleep(100 * time.Millisecond)

				content, _ := os.ReadFile(filepath.Join("logs", "amsh.log"))
				So(string(content), ShouldContainSubstring, "debug msg")
			})

			Convey("Info should write keyvals to log file", func() {
				Info("info msg", "k", "v")
				time.Sleep(100 * time.Millisecond)

				content, _ := os.ReadFile(filepath.Join("logs", "amsh.log"))
				So(string(content), ShouldContainSubstring, "info msg")
			})

			Convey("Warn should write keyvals to log file", func() {
				Warn("warn msg", "k", "v")
				time.Sleep(100 * time.Millisecond)

				content, _ := os.ReadFile(filepath.Join("logs", "amsh.log"))
				So(string(content), ShouldContainSubstring, "warn msg")
			})

			Convey("Error should write to log file and preserve error", func() {
				testErr := errors.New("logged error")
				result := Error(testErr)
				time.Sleep(100 * time.Millisecond)

				So(result, ShouldEqual, testErr)
				content, _ := os.ReadFile(filepath.Join("logs", "amsh.log"))
				So(string(content), ShouldContainSubstring, "logged error")
			})
		})

		Convey("writeToLog should be a no-op when LOGFILE is not set", func() {
			os.Unsetenv("LOGFILE")
			So(func() { writeToLog("test") }, ShouldNotPanic)
		})

		Convey("ErrorSafe should log without returning", func() {
			So(func() { ErrorSafe(errors.New("safe error")) }, ShouldNotPanic)
		})

		Convey("ErrorSafe should be a no-op for nil", func() {
			So(func() { ErrorSafe(nil) }, ShouldNotPanic)
		})
	})
}

// ---------------------------------------------------------------------------
// Logger benchmarks
// ---------------------------------------------------------------------------

/*
swapLogger replaces the global logger with one that writes to io.Discard
(removing I/O variance from timing) and returns a restore function.
*/
func swapLogger() func() {
	old := logger
	logger = log.NewWithOptions(io.Discard, log.Options{
		ReportCaller:    true,
		CallerOffset:    1,
		ReportTimestamp: true,
		TimeFormat:      time.TimeOnly,
		Level:           log.DebugLevel,
	})

	return func() { logger = old }
}

func BenchmarkErrorNil(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		Error(nil)
	}
}

func BenchmarkError(b *testing.B) {
	restore := swapLogger()
	defer restore()

	testErr := errors.New("bench error")
	b.ReportAllocs()

	for b.Loop() {
		Error(testErr)
	}
}

func BenchmarkErrorWithCaller(b *testing.B) {
	old := logger
	logger = log.NewWithOptions(io.Discard, log.Options{
		ReportCaller:    true,
		ReportTimestamp: true,
		TimeFormat:      time.TimeOnly,
		Level:           log.DebugLevel,
	})
	defer func() { logger = old }()

	testErr := errors.New("bench error")
	b.ReportAllocs()

	for b.Loop() {
		Error(testErr)
	}
}

func BenchmarkErrorNoCaller(b *testing.B) {
	old := logger
	logger = log.NewWithOptions(io.Discard, log.Options{
		ReportCaller:    false,
		ReportTimestamp: true,
		TimeFormat:      time.TimeOnly,
		Level:           log.DebugLevel,
	})
	defer func() { logger = old }()

	testErr := errors.New("bench error")
	b.ReportAllocs()

	for b.Loop() {
		Error(testErr)
	}
}

func BenchmarkErrorSafe(b *testing.B) {
	restore := swapLogger()
	defer restore()

	testErr := errors.New("bench error")
	b.ReportAllocs()

	for b.Loop() {
		ErrorSafe(testErr)
	}
}

func BenchmarkDebug(b *testing.B) {
	restore := swapLogger()
	defer restore()
	b.ReportAllocs()

	for b.Loop() {
		Debug("test message", "key", "value")
	}
}

func BenchmarkWarn(b *testing.B) {
	restore := swapLogger()
	defer restore()
	b.ReportAllocs()

	for b.Loop() {
		Warn("test warning", "key", "value")
	}
}

func BenchmarkDebugNoFileLog(b *testing.B) {
	restore := swapLogger()
	defer restore()

	old := logFile
	logFile = nil
	defer func() { logFile = old }()

	b.ReportAllocs()

	for b.Loop() {
		Debug("test message", "key", "value")
	}
}

func BenchmarkHandleErrorPath(b *testing.B) {
	restore := swapLogger()
	defer restore()

	testErr := errors.New("handle bench")
	b.ReportAllocs()

	for b.Loop() {
		state := NewState("bench")
		state.Handle(testErr)
	}
}

func BenchmarkHandleErrorPathExistingState(b *testing.B) {
	restore := swapLogger()
	defer restore()

	testErr := errors.New("handle bench")
	state := NewState("bench")
	b.ReportAllocs()

	for b.Loop() {
		state.Reset()
		state.Handle(testErr)
	}
}

func BenchmarkLogDiscard(b *testing.B) {
	restore := swapLogger()
	defer restore()
	b.ReportAllocs()

	for b.Loop() {
		Log("message %d", 42)
	}
}

func BenchmarkErrorChainPreserved(b *testing.B) {
	restore := swapLogger()
	defer restore()

	sentinel := errors.New("sentinel")
	wrapped := fmt.Errorf("wrap: %w", sentinel)
	b.ReportAllocs()

	for b.Loop() {
		result := Error(wrapped)
		if !errors.Is(result, sentinel) {
			b.Fatal("chain broken")
		}
	}
}

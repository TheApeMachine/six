package console

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/log"
	config "github.com/theapemachine/six/pkg/system/core"
)

/*
logger is the package-level Logger instance.
It is bootstrapped in init so all console output uses a single shared instance.
*/
var logger *Logger

var traceEnabled bool

/*
init bootstraps the package-level logger and caches SIX_TRACE.
On failure, falls back to stderr-only logging so the application can still run.
*/
func init() {
	traceEnabled = os.Getenv("SIX_TRACE") == "1"
	l, err := New()
	if err != nil {
		// Fallback: stderr-only logger, no file tracing
		l = &Logger{
			handle: *log.NewWithOptions(
				os.Stderr,
				log.Options{
					ReportTimestamp: true,
					ReportCaller:    true,
				},
			),
			traceHandle: *log.NewWithOptions(
				os.Stderr,
				log.Options{
					ReportTimestamp: false,
					ReportCaller:    false,
					Level:           log.DebugLevel,
				},
			),
		}
	}
	logger = l
}

/*
Logger is a dual-handle logger that writes user-facing output to stderr
and verbose trace output to six.log.
It exists so the application can separate actionable messages from debug noise.
*/
type Logger struct {
	handle      log.Logger
	traceHandle log.Logger
	file        *os.File
}

/*
Close closes the underlying log file.
*/
func (l *Logger) Close() error {
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

/*
New instantiates a Logger with stderr for main output and six.log for trace output.
Returns an error if the log file cannot be opened.
*/
func New() (*Logger, error) {
	file, err := os.OpenFile(
		filepath.Join(config.System.ProjectRoot, "six.log"),
		os.O_APPEND|os.O_WRONLY|os.O_CREATE,
		0666,
	)
	if err != nil {
		return nil, err
	}

	l := &Logger{
		handle: *log.NewWithOptions(
			os.Stderr,
			log.Options{
				ReportTimestamp: true,
				ReportCaller:    true,
			},
		),
		// traceHandle writes verbose trace output to the provided file
		traceHandle: *log.NewWithOptions(
			file,
			log.Options{
				ReportTimestamp: false,
				ReportCaller:    false,
				Level:           log.DebugLevel,
			},
		),
		file: file,
	}

	return l, nil
}

/*
Info logs a message at info level to the main handle.
*/
func Info(msg string, keyvals ...any) {
	logger.handle.Info(msg, keyvals...)
}

/*
Trace logs a message at debug level to the trace handle (six.log).
*/
func Trace(msg string, keyvals ...any) {
	logger.traceHandle.Debug(msg, keyvals...)
}

/*
Error logs an error to the main handle and returns it unchanged.
Returns nil immediately if err is nil.
*/
func Error(err error, keyvals ...any) error {
	if err == nil {
		return nil
	}

	logger.handle.Error(err, keyvals...)

	return err
}

/*
Warn logs a message at warn level to the main handle.
*/
func Warn(msg fmt.Stringer, keyvals ...any) {
	logger.handle.Warn(msg.String(), keyvals...)
}

/*
Debug logs a message at debug level to the main handle.
*/
func Debug(msg string, keyvals ...any) {
	logger.handle.Debug(msg, keyvals...)
}

/*
SetLevel changes the main handle's minimum log level.
*/
func SetLevel(level log.Level) {
	logger.handle.SetLevel(level)
}

/*
IsTraceEnabled reports whether trace logging is enabled (e.g. via SIX_TRACE=1).
Call before expensive Trace calls to avoid cost when tracing is off.
*/
func IsTraceEnabled() bool {
	return traceEnabled
}

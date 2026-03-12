package console

import (
	"os"
	"path/filepath"

	"github.com/charmbracelet/log"
)

/*
logger is the package-level Logger instance.
It is bootstrapped in init so all console output uses a single shared instance.
*/
var logger *Logger

/*
init bootstraps the package-level logger.
On failure, falls back to stderr-only logging so the application can still run.
*/
func init() {
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
}

/*
New instantiates a Logger with stderr for main output and six.log for trace output.
Returns an error if the working directory cannot be determined or the log file cannot be opened.
*/
func New() (*Logger, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	// Open the log file for appending using an absolute path
	file, err := os.OpenFile(
		filepath.Join(wd, "six.log"),
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
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
		// Always initialize traceHandle to a safe fallback (stderr)
		traceHandle: *log.NewWithOptions(
			file,
			log.Options{
				ReportTimestamp: false,
				ReportCaller:    false,
				Level:           log.DebugLevel,
			},
		),
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
func Warn(msg string, keyvals ...any) {
	logger.handle.Warn(msg)
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

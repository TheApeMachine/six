package console

import (
	"io"
	"os"
	"path/filepath"

	"github.com/charmbracelet/log"
)

var logger *Logger

func init() {
	logger = New()
}

type Logger struct {
	handle      log.Logger
	traceHandle log.Logger
}

func New() *Logger {
	var out io.Writer = os.Stderr

	wd, err := os.Getwd()

	if err != nil {
		panic(err)
	}

	// Open the log file for appending using an absolute path
	file, err := os.OpenFile(filepath.Join(wd, "six.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err == nil {
		// Output to both stderr and the file
		out = io.MultiWriter(os.Stderr, file)
	}

	l := &Logger{
		handle: *log.NewWithOptions(
			out,
			log.Options{
				ReportTimestamp: true,
				ReportCaller:    true,
			},
		),
		// Always initialize traceHandle to a safe fallback (stderr)
		traceHandle: *log.NewWithOptions(
			os.Stderr,
			log.Options{
				ReportTimestamp: true,
				ReportCaller:    true,
				Level:           log.DebugLevel,
			},
		),
	}

	if err == nil {
		l.traceHandle = *log.NewWithOptions(
			file,
			log.Options{
				ReportTimestamp: true,
				ReportCaller:    true,
				Level:           log.DebugLevel,
			},
		)
	}

	return l
}

func Info(msg string, keyvals ...any) {
	logger.handle.Info(msg, keyvals...)
}

func Trace(msg string, keyvals ...any) {
	logger.traceHandle.Debug(msg, keyvals...)
}

func Error(err error, keyvals ...any) error {
	if err == nil {
		return nil
	}

	logger.handle.Error(err, keyvals...)

	return err
}

func Warn(msg string) {
	logger.handle.Warn(msg)
}

func Debug(msg string, keyvals ...any) {
	logger.handle.Debug(msg, keyvals...)
}

func SetLevel(level log.Level) {
	logger.handle.SetLevel(level)
}

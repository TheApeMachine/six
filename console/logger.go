package console

import (
	"io"
	"os"

	"github.com/charmbracelet/log"
)

var logger *Logger

func init() {
	logger = New()
}

type Logger struct {
	handle log.Logger
}

func New() *Logger {
	var out io.Writer = os.Stderr

	// Open the log file for appending using an absolute path
	file, err := os.OpenFile("/Users/theapemachine/go/src/github.com/theapemachine/six/six.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err == nil {
		// Output to both stderr and the file
		out = io.MultiWriter(os.Stderr, file)
	}

	return &Logger{
		handle: *log.NewWithOptions(
			out,
			log.Options{
				ReportTimestamp: true,
				ReportCaller:    true,
			},
		),
	}
}

func Info(msg string, keyvals ...any) {
	logger.handle.Info(msg, keyvals...)
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

func Debug(msg string) {
	logger.handle.Debug(msg)
}

func SetLevel(level log.Level) {
	logger.handle.SetLevel(level)
}

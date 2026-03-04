package console

import (
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
	return &Logger{
		handle: *log.NewWithOptions(
			os.Stderr,
			log.Options{
				ReportTimestamp: true,
				ReportCaller:    true,
			},
		),
	}
}

func Info(msg string) {
	logger.handle.Info(msg)
}

func Error(err error) error {
	if err == nil {
		return nil
	}

	logger.handle.Error(err)

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

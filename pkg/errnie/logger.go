package errnie

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/muesli/termenv"
	"github.com/spf13/viper"
)

var (
	logFile   *os.File
	logFileMu sync.Mutex

	logger = log.NewWithOptions(os.Stderr, log.Options{
		ReportCaller:    true,
		CallerOffset:    16,
		ReportTimestamp: true,
		TimeFormat:      time.TimeOnly,
		Level:           log.DebugLevel,
	})
)

func init() {
	/*
		IDE test panels and log captures treat stderr as plain text, so ANSI
		sequences show up as junk. Real terminals still get color when running
		the normal binary (testing.Testing() is false).
	*/
	if testing.Testing() {
		logger.SetColorProfile(termenv.Ascii)
	}
}

/*
InitLogger configures log styles, sets log levels, and initializes
file logging when LOGFILE=true.
*/
func InitLogger() {
	if os.Getenv("LOGFILE") == "true" {
		initLogFile()
	}

	setLogLevel()
}

/*
setLogLevel reads the Viper "loglevel" key and configures the global logger.
*/
func setLogLevel() {
	switch viper.GetString("loglevel") {
	case "trace", "debug":
		logger.SetLevel(log.DebugLevel)
	case "info":
		logger.SetLevel(log.InfoLevel)
	case "warn":
		logger.SetLevel(log.WarnLevel)
	case "error":
		logger.SetLevel(log.ErrorLevel)
	default:
		logger.SetLevel(log.DebugLevel)
	}
}

/*
initLogFile opens (or creates) the log file under $CWD/logs/amsh.log.
*/
func initLogFile() {
	wd, err := os.Getwd()
	if err != nil {
		logger.Warn("Failed to get working directory", "error", err)
		return
	}

	logDir := filepath.Join(wd, "logs")

	if err := os.MkdirAll(logDir, 0755); err != nil {
		logger.Warn("Failed to create log directory", "error", err)
		return
	}

	logFilePath := filepath.Join(logDir, "amsh.log")

	logFile, err = os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		logger.Warn("Failed to open log file", "error", err)
		return
	}

	logger.Debug("Log file successfully initialized", "path", logFilePath)
}

/*
Error logs the error and returns it unchanged, preserving the full
error chain for errors.Is/errors.As downstream.
*/
func Error(err error, keyvals ...any) error {
	if err == nil {
		return nil
	}

	logger.Error(err, keyvals...)

	if logFile != nil {
		writeToLog(append(keyvals, err)...)
	}

	return err
}

/*
writeToLog appends to the log file when LOGFILE=true.
*/
func writeToLog(msg ...any) {
	if len(msg) == 0 {
		return
	}

	logFileMu.Lock()
	defer logFileMu.Unlock()

	var builder strings.Builder

	for _, msg := range msg {
		builder.WriteString(fmt.Sprintf("%v", msg))
	}

	formattedMessage := builder.String()

	_, err := logFile.WriteString(formattedMessage)
	if err != nil {
		logger.Warn("Failed to write to log file", "error", err)
	}

	if syncErr := logFile.Sync(); syncErr != nil {
		logger.Warn("Failed to sync log file", "error", syncErr)
	}
}

package errnie

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/davecgh/go-spew/spew"
	"github.com/spf13/viper"
)

var (
	logFile   *os.File
	logFileMu sync.Mutex

	logger = log.NewWithOptions(os.Stderr, log.Options{
		ReportCaller:    true,
		CallerOffset:    1,
		ReportTimestamp: true,
		TimeFormat:      time.TimeOnly,
		Level:           log.DebugLevel,
	})
)

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
Log formats and writes a message to the log file.
*/
func Log(format string, v ...any) {
	message := fmt.Sprintf(format, v...)

	if message == "" {
		return
	}

	if logFile != nil {
		writeToLog(message)
	}
}

/*
Raw is a full decomposition of the object via spew.
*/
func Raw(v ...any) {
	spew.Dump(v...)

	if logFile != nil {
		writeToLog(spew.Sdump(v...))
	}
}

/*
Trace logs at debug level to the global logger.
*/
func Trace(v ...any) {
	logger.Debug(v[0], v[1:]...)

	if logFile != nil {
		writeToLog(fmt.Sprintf("%v", v))
	}
}

/*
Debug logs at debug level.
*/
func Debug(msg any, keyvals ...any) {
	logger.Debug(msg, keyvals...)

	if logFile != nil {
		writeToLog(append(keyvals, msg)...)
	}
}

/*
Info logs at info level.
*/
func Info(msg any, keyvals ...any) {
	logger.Info(msg, keyvals...)

	if logFile != nil {
		writeToLog(append(keyvals, msg)...)
	}
}

/*
Warn logs at warn level.
*/
func Warn(msg any, keyvals ...any) {
	logger.Warn(msg, keyvals...)

	if logFile != nil {
		writeToLog(append(keyvals, msg)...)
	}
}

/*
ErrorSafe logs the error without returning a new one. Used by SafeMust
and Handle where the caller manages error state independently.
*/
func ErrorSafe(err error, v ...any) {
	if err == nil {
		return
	}

	logger.Error(err.Error(), v...)

	if logFile != nil {
		writeToLog(append(v, err)...)
	}
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

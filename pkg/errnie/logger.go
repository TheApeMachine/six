package errnie

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/acarl005/stripansi"
	"github.com/charmbracelet/lipgloss"
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
Initialize logging system by configuring log styles, setting log levels,
and initializing log files if applicable.
*/
func InitLogger() {
	fmt.Printf("LOGFILE=%s\n", os.Getenv("LOGFILE"))
	fmt.Printf("NOCONSOLE=%s\n", os.Getenv("NOCONSOLE"))

	if os.Getenv("LOGFILE") == "true" {
		// Initialize the log file
		initLogFile()

		if logFile == nil {
			fmt.Println("WARNING: Log file initialization failed!")
		}
	}

	// Set log level based on configuration
	setLogLevel()

	if os.Getenv("LOGGOROUTINES") == "true" {
		// Periodic routine to print the number of active goroutines
		go func() {
			for range time.Tick(time.Second * 5) {
				logger.Debug("active goroutines", "count", runtime.NumGoroutine())
			}
		}()
	}
}

/*
Set the appropriate logging level from Viper configuration.
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
Initialize the log file by creating or overwriting the log file.
Handles any errors during initialization gracefully.
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
Log a formatted message to the standard logger as well as to the log file.
*/
func Log(format string, v ...interface{}) {
	message := fmt.Sprintf(format, v...)
	if message == "" {
		return
	}
	writeToLog(message)
}

/*
Raw is a full decomposition of the object passed in.
It will print the object into the console using spew, and it will write the object to the logfile.
*/
func Raw(v ...interface{}) {
	formatted := spew.Sdump(v...)
	if os.Getenv("NOCONSOLE") != "true" {
		spew.Dump(v...)
	}

	writeToLog(formatted)
}

/*
Trace logs a trace message to the logger.
*/
func Trace(v ...interface{}) {
	if os.Getenv("NOCONSOLE") != "true" {
		logger.Debug(v[0], v[1:]...)
	}

	writeToLog(fmt.Sprintf("%v", v))
}

/*
Debug logs a debug message to the logger.
*/
func Debug(msg interface{}, keyvals ...interface{}) {
	if os.Getenv("NOCONSOLE") != "true" {
		logger.Debug(msg, keyvals...)
	}

	writeToLog(formatLogMessage(msg, keyvals...))
}

/*
Info logs an info message to the logger.
*/
func Info(msg interface{}, keyvals ...interface{}) {
	if os.Getenv("NOCONSOLE") != "true" {
		logger.Info(msg, keyvals...)
	}

	writeToLog(formatLogMessage(msg, keyvals...))
}

/*
Warn logs a warn message to the logger.
*/
func Warn(msg interface{}, keyvals ...interface{}) {
	if os.Getenv("NOCONSOLE") != "true" {
		logger.Warn(msg, keyvals...)
	}

	writeToLog(formatLogMessage(msg, keyvals...))
}

/*
ErrorSafe logs a simple version of the error, used in SafeMust.
*/
func ErrorSafe(err error, v ...interface{}) {
	if err == nil {
		return
	}

	if os.Getenv("NOCONSOLE") != "true" {
		logger.Error(err.Error(), v...)
	}

	writeToLog(err.Error())

}

/*
Error logs the error and returns it, useful for inline error logging and returning.

Example usage:

	err := someFunction()
	if err != nil {
		return Error(err, "additional context")
	}
*/
func Error(err error, keyvals ...interface{}) error {
	if err == nil {
		return nil
	}

	if os.Getenv("NOCONSOLE") != "true" {
		logger.Error(err, keyvals...)
	}

	enriched := err.Error()

	if os.Getenv("NOSTACK") != "true" {
		enriched += getStackTrace()

		if os.Getenv("NOSNIPPET") != "true" {
			enriched += getFileSnippet(err.Error())
		}
	}

	writeToLog(enriched)

	return errors.New(enriched)
}

/*
Write a log message to the log file, ensuring thread safety.
*/
func writeToLog(message string) {
	if os.Getenv("LOGFILE") != "true" || message == "" || logFile == nil {
		return
	}

	logFileMu.Lock()
	defer logFileMu.Unlock()

	// Strip ANSI escape codes and add a timestamp
	formattedMessage := fmt.Sprintf("%s", stripansi.Strip(message))

	_, err := logFile.WriteString(formattedMessage)
	if err != nil {
		logger.Warn("Failed to write to log file", "error", err)
	}

	// Ensure the write is flushed to disk
	if syncErr := logFile.Sync(); syncErr != nil {
		logger.Warn("Failed to sync log file", "error", syncErr)
	}
}

/*
Retrieve and format a stack trace from the current execution point.
*/
func getStackTrace() string {
	const depth = 32
	var pcs [depth]uintptr
	n := runtime.Callers(3, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])

	var trace strings.Builder
	for {
		frame, more := frames.Next()
		if !more {
			break
		}

		funcName := frame.Function
		if lastSlash := strings.LastIndexByte(funcName, '/'); lastSlash >= 0 {
			funcName = funcName[lastSlash+1:]
		}
		funcName = strings.Replace(funcName, ".", ":", 1)

		line := fmt.Sprintf("%s at %s(line %d)\n",
			lipgloss.NewStyle().Foreground(lipgloss.Color("#6E95F7")).Render(funcName),
			lipgloss.NewStyle().Foreground(lipgloss.Color("#06C26F")).Render(filepath.Base(frame.File)),
			frame.Line,
		)
		trace.WriteString(line)
	}

	return "\n===[STACK TRACE]===\n" + trace.String() + "===[/STACK TRACE]===\n"
}

/*
Retrieve and return a code snippet surrounding the given line in the provided file.
*/
func getCodeSnippet(file string, line, radius int) string {
	if file == "" {
		return ""
	}

	fileHandle, err := os.Open(file)
	if err != nil {
		logger.Warn("Failed to open file for code snippet", "file", file, "error", err)
		return ""
	}
	defer fileHandle.Close()

	scanner := bufio.NewScanner(fileHandle)
	currentLine := 1
	var snippet strings.Builder

	for scanner.Scan() {
		if currentLine >= line-radius && currentLine <= line+radius {
			prefix := "  "
			if currentLine == line {
				prefix = "> "
			}
			snippet.WriteString(fmt.Sprintf("%s%d: %s\n", prefix, currentLine, scanner.Text()))
		}
		currentLine++
	}

	if err := scanner.Err(); err != nil {
		logger.Warn("Failed to read from code snippet file", "file", file, "error", err)
		return ""
	}

	return snippet.String()
}

/*
formatLogMessage applies fmt.Sprintf formatting to produce a clean log file entry.
*/
func formatLogMessage(msg interface{}, keyvals ...interface{}) string {
	msgStr := fmt.Sprintf("%v", msg)

	if len(keyvals) > 0 {
		msgStr = fmt.Sprintf(msgStr, keyvals...)
	}

	return msgStr + "\n"
}

/*
getFileSnippet reads the file at the given path if it exists, returning its
contents (up to 20 lines) for inclusion in an enriched error message.
*/
func getFileSnippet(path string) string {
	if path == "" {
		return ""
	}

	info, statErr := os.Stat(path)
	if statErr != nil || info.IsDir() {
		return ""
	}

	fileHandle, openErr := os.Open(path)
	if openErr != nil {
		return ""
	}
	defer fileHandle.Close()

	scanner := bufio.NewScanner(fileHandle)
	var snippet strings.Builder
	lineCount := 0

	snippet.WriteString("\n===[CODE SNIPPET]===\n")

	for scanner.Scan() && lineCount < 20 {
		lineCount++
		snippet.WriteString(fmt.Sprintf("  %d: %s\n", lineCount, scanner.Text()))
	}

	snippet.WriteString("===[/CODE SNIPPET]===\n")

	return snippet.String()
}


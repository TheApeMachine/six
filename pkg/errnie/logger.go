package errnie

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/muesli/termenv"
	"github.com/spf13/viper"
)

var (
	logFile   *os.File
	logFileMu sync.Mutex

	traceFile   *os.File
	traceFileMu sync.Mutex
	traceInitMu sync.Mutex

	logger = &ErrnieLogger{
		Logger: log.NewWithOptions(os.Stderr, log.Options{
			ReportCaller:    true,
			CallerOffset:    16,
			ReportTimestamp: true,
			TimeFormat:      time.TimeOnly,
			Level:           log.DebugLevel,
		}),
	}
)

type ErrnieLogger struct {
	*log.Logger
}

func init() {
	// Match charmbracelet/log output to stderr: no ANSI when not a TTY (tests,
	// pipes, redirects) or when env disables color (NO_COLOR, CI, etc.).
	logger.SetColorProfile(termenv.NewOutput(os.Stderr).EnvColorProfile())
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

func Logger() *ErrnieLogger {
	return logger
}

/*
Read implements io.Reader.
*/
func (logger *ErrnieLogger) Read(p []byte) (n int, err error) {
	return logFile.Read(p)
}

/*
Write implements io.Writer.
*/
func (logger *ErrnieLogger) Write(p []byte) (n int, err error) {
	return logFile.Write(p)
}

/*
Close implements io.Closer.
*/
func (logger *ErrnieLogger) Close() error {
	return logFile.Close()
}

/*
Info logs the info message.
*/
func Info(msg string, keyvals ...any) {
	logger.Info(msg, keyvals...)
}

/*
Debug logs the debug message.
*/
func Debug(msg string, keyvals ...any) {
	logger.Debug(msg, keyvals...)
}

/*
Trace appends a line to a log file (default ./trace.log under the process
working directory, so e.g. package-dir/trace.log when running `go test`).

Override the path with env TRACE_LOG (absolute or relative to cwd).

If the file cannot be opened, the line is written to stderr so output is
never silently dropped.
*/
func Trace(msg string, keyvals ...any) {
	parts := append(keyvals, msg)
	ensureTraceFile()
	line := formatTraceLine(parts)

	if traceFile != nil {
		writeTraceLine(line)
		return
	}

	fmt.Fprintln(os.Stderr, line)
}

/*
Warn logs the warn message.
*/
func Warn(msg string, keyvals ...any) {
	logger.Warn(msg, keyvals...)
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

func ensureTraceFile() {
	traceInitMu.Lock()
	defer traceInitMu.Unlock()

	if traceFile != nil {
		return
	}

	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "errnie trace: getwd: %v\n", err)
		logger.Warn("trace: failed to get working directory", "error", err)
		return
	}

	path := os.Getenv("TRACE_LOG")
	if path == "" {
		path = filepath.Join(wd, "trace.log")
	}

	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "errnie trace: mkdir %q: %v\n", dir, err)
			logger.Warn("trace: failed to create log directory", "error", err, "path", dir)
			return
		}
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "errnie trace: open %q: %v\n", path, err)
		logger.Warn("trace: failed to open trace log", "error", err, "path", path)
		return
	}

	traceFile = f
	logger.Debug("Trace log initialized", "path", path)
}

/*
writeToLog appends to the log file when LOGFILE=true.
*/
func writeToLog(msg ...any) {
	if logFile == nil {
		return
	}

	appendLineToFile(&logFile, &logFileMu, msg)
}

func formatTraceLine(parts []any) string {
	if len(parts) == 0 {
		return ""
	}
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(fmt.Sprintf("%v", p))
	}
	return b.String()
}

func writeTraceLine(line string) {
	traceFileMu.Lock()
	defer traceFileMu.Unlock()

	if traceFile == nil {
		return
	}

	if line == "" {
		line = "\n"
	} else {
		line += "\n"
	}

	_, err := traceFile.WriteString(line)
	if err != nil {
		logger.Warn("trace: write failed", "error", err)
		fmt.Fprintf(os.Stderr, "errnie trace: write: %v\n", err)
		return
	}

	if syncErr := traceFile.Sync(); syncErr != nil {
		logger.Warn("trace: sync failed", "error", syncErr)
	}
}

func appendLineToFile(f **os.File, mu *sync.Mutex, parts []any) {
	if len(parts) == 0 || *f == nil {
		return
	}

	line := formatTraceLine(parts)
	if line == "" {
		line = "\n"
	} else {
		line += "\n"
	}

	mu.Lock()
	defer mu.Unlock()

	_, err := (*f).WriteString(line)
	if err != nil {
		logger.Warn("Failed to write to log file", "error", err)
		return
	}

	if syncErr := (*f).Sync(); syncErr != nil {
		logger.Warn("Failed to sync log file", "error", syncErr)
	}
}

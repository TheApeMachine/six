package errnie

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	logFile   *os.File
	logFileMu sync.Mutex

	traceFile   *os.File
	traceFileMu sync.Mutex
	traceInitMu sync.Mutex

	loggerMu sync.RWMutex
	logger   = mustNewDefaultLogger()

	initErr error

	loggingCfgMu sync.RWMutex
	loggingCfg   LoggingConfig
)

func mustNewDefaultLogger() *ErrnieLogger {
	z, err := buildLogger(zapcore.InfoLevel, ElasticsearchConfig{})
	if err != nil {
		initErr = err
		enc := zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())
		core := zapcore.NewCore(enc, zapcore.Lock(os.Stderr), zapcore.InfoLevel)
		z = zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))
	}
	return &ErrnieLogger{Logger: z}
}

func buildLogger(enab zapcore.LevelEnabler, escfg ElasticsearchConfig) (*zap.Logger, error) {
	consoleCore := zapcore.NewCore(
		zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig()),
		zapcore.Lock(os.Stderr),
		enab,
	)

	cores := []zapcore.Core{consoleCore}

	esOut, err := newElasticsearchClientAndSink(escfg)
	initErr = err
	if err != nil {
		return nil, err
	}
	if esOut != nil {
		jsonEnc := zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())
		esCore := zapcore.NewCore(jsonEnc, zapcore.AddSync(esOut), enab)
		cores = append(cores, esCore)
	}

	core := zapcore.NewTee(cores...)
	return zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1)), nil
}

func zapLevelFromViper() zapcore.Level {
	switch strings.ToLower(viper.GetString("loglevel")) {
	case "trace", "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.DebugLevel
	}
}

/*
InitLogger rebuilds the global zap logger from cfg (loaded via core.LoadLoggingConfig
and the "logging" section of config.yml). Call after Viper config is loaded.
*/
func InitLogger(cfg LoggingConfig) {
	loggingCfgMu.Lock()
	loggingCfg = cfg
	loggingCfgMu.Unlock()

	if cfg.Logfile {
		initLogFile()
	}

	lvl := zapLevelFromViper()

	z, err := buildLogger(lvl, cfg.Elasticsearch)
	if err != nil {
		fmt.Fprintf(os.Stderr, "errnie: logger init: %v (using stderr-only fallback)\n", err)
		enc := zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())
		core := zapcore.NewCore(enc, zapcore.Lock(os.Stderr), lvl)
		z = zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))
		initErr = err
	}

	loggerMu.Lock()
	old := logger.Logger
	logger = &ErrnieLogger{Logger: z}
	loggerMu.Unlock()
	_ = old.Sync()

	if cfg.Elasticsearch.Enabled {
		if initErr != nil {
			fmt.Fprintf(os.Stderr, "errnie: elasticsearch not active: %v\n", initErr)
		} else {
			idx := strings.TrimSpace(cfg.Elasticsearch.Index)
			if idx == "" {
				idx = "six-logs"
			}
			fmt.Fprintf(os.Stderr, "errnie: elasticsearch indexing enabled (index=%q)\n", idx)
		}
	}
}

// Sync flushes any buffered stdout/stderr log I/O (see zap.Logger.Sync).
func Sync() error {
	loggerMu.RLock()
	z := logger.Logger
	loggerMu.RUnlock()
	if z == nil {
		return nil
	}
	return z.Sync()
}

// Shutdown closes the Elasticsearch bulk indexer (if enabled) and flushes zap. Call from main on exit.
func Shutdown(ctx context.Context) error {
	closeElasticsearchSink(ctx)
	return Sync()
}

// InitError returns the last error from enabling Elasticsearch, if any.
func InitError() error { return initErr }

type ErrnieLogger struct {
	*zap.Logger
}

func Logger() *ErrnieLogger {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	return logger
}

func (l *ErrnieLogger) Read(p []byte) (n int, err error) {
	if logFile == nil {
		return 0, io.EOF
	}
	return logFile.Read(p)
}

func (l *ErrnieLogger) Write(p []byte) (n int, err error) {
	if logFile == nil {
		return 0, fmt.Errorf("no log file configured")
	}
	return logFile.Write(p)
}

func (l *ErrnieLogger) Close() error {
	if logFile == nil {
		return nil
	}
	return logFile.Close()
}

func initLogFile() {
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "errnie: getwd: %v\n", err)
		return
	}

	logDir := filepath.Join(wd, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "errnie: mkdir logs: %v\n", err)
		return
	}

	logFilePath := filepath.Join(logDir, "amsh.log")
	logFile, err = os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "errnie: open log file: %v\n", err)
		return
	}
}

func keyvalsToFields(keyvals []any) []zap.Field {
	if len(keyvals) == 0 {
		return nil
	}
	fields := make([]zap.Field, 0, (len(keyvals)+1)/2)
	for i := 0; i < len(keyvals); i += 2 {
		if i+1 >= len(keyvals) {
			fields = append(fields, zap.Any(fmt.Sprintf("key_%d", i), keyvals[i]))
			continue
		}
		k, ok := keyvals[i].(string)
		if !ok {
			k = fmt.Sprint(keyvals[i])
		}
		fields = append(fields, zap.Any(k, keyvals[i+1]))
	}
	return fields
}

func logZ(level zapcore.Level, msg string, keyvals []any) {
	loggerMu.RLock()
	z := logger.Logger
	loggerMu.RUnlock()
	if z == nil {
		return
	}
	fields := keyvalsToFields(keyvals)
	switch level {
	case zapcore.DebugLevel:
		z.Debug(msg, fields...)
	case zapcore.InfoLevel:
		z.Info(msg, fields...)
	case zapcore.WarnLevel:
		z.Warn(msg, fields...)
	case zapcore.ErrorLevel:
		z.Error(msg, fields...)
	default:
		z.Info(msg, fields...)
	}
}

func Info(msg string, keyvals ...any)  { logZ(zapcore.InfoLevel, msg, keyvals) }
func Debug(msg string, keyvals ...any) { logZ(zapcore.DebugLevel, msg, keyvals) }
func Warn(msg string, keyvals ...any)  { logZ(zapcore.WarnLevel, msg, keyvals) }

/*
Trace logs at debug level with component=trace for structured sinks, and optionally
appends a plain line to logging.trace.path or ./trace.log when path is empty.
*/
func Trace(msg string, keyvals ...any) {
	formatted := traceKeyvalsFormatted(keyvals)
	fields := append(keyvalsToFields(formatted), zap.String("component", "trace"))
	loggerMu.RLock()
	z := logger.Logger
	loggerMu.RUnlock()
	if z != nil {
		z.Debug(msg, fields...)
	}
	ensureTraceFile()
	line := buildTraceLine(msg, formatted)
	if traceFile != nil {
		writeTraceLine(line)
		return
	}
	fmt.Fprintln(os.Stderr, line)
}

func Error(err error, keyvals ...any) error {
	if err == nil {
		return nil
	}

	kv := append([]any{}, keyvals...)
	if IsReschedulable(err) {
		kv = append(kv, "reschedulable", true)
	}
	if ctx := HasContext(err); ctx != nil && ctx.Err() != nil {
		kv = append(kv, "context_err", ctx.Err())
	}

	fields := append([]zap.Field{zap.Error(err)}, keyvalsToFields(kv)...)
	loggerMu.RLock()
	z := logger.Logger
	loggerMu.RUnlock()
	if z != nil {
		z.Error(err.Error(), fields...)
	}

	if logFile != nil {
		writeToLog(append(kv, err)...)
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
		return
	}

	loggingCfgMu.RLock()
	cfgPath := strings.TrimSpace(loggingCfg.Trace.Path)
	loggingCfgMu.RUnlock()

	path := cfgPath
	if path == "" {
		path = filepath.Join(wd, "trace.log")
	}

	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "errnie trace: mkdir %q: %v\n", dir, err)
			return
		}
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "errnie trace: open %q: %v\n", path, err)
		return
	}

	traceFile = f
	Debug("Trace log file initialized", "path", path)
}

func writeToLog(msg ...any) {
	if logFile == nil {
		return
	}
	appendLineToFile(logFile, &logFileMu, msg)
}

func buildTraceLine(msg string, keyvals []any) string {
	if len(keyvals) == 0 {
		return msg
	}
	var b strings.Builder
	b.WriteString(msg)
	for i := 0; i < len(keyvals); i += 2 {
		b.WriteByte(' ')
		if i+1 < len(keyvals) {
			b.WriteString(fmt.Sprintf("%v=%v", keyvals[i], keyvals[i+1]))
		} else {
			b.WriteString(fmt.Sprintf("%v", keyvals[i]))
		}
	}
	return b.String()
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
		fmt.Fprintf(os.Stderr, "errnie trace: write: %v\n", err)
		return
	}

	if syncErr := traceFile.Sync(); syncErr != nil {
		fmt.Fprintf(os.Stderr, "errnie trace: sync: %v\n", syncErr)
	}
}

func appendLineToFile(f *os.File, mu *sync.Mutex, parts []any) {
	if len(parts) == 0 || f == nil {
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

	_, err := f.WriteString(line)
	if err != nil {
		return
	}

	_ = f.Sync()
}

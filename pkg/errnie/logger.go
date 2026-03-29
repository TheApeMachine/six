package errnie

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	logFile *os.File

	traceFile   *os.File
	traceInitMu sync.Mutex
	traceQueue  chan string

	loggerPtr atomic.Pointer[ErrnieLogger]

	initErr error

	loggingCfg atomic.Value
)

func init() {
	loggerPtr.Store(mustNewDefaultLogger())
	loggingCfg.Store(LoggingConfig{})
}

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
	if err != nil {
		initErr = err
		return nil, err
	}
	if esOut != nil {
		esEncCfg := zap.NewProductionEncoderConfig()
		esEncCfg.TimeKey = "timestamp"
		esEncCfg.EncodeTime = zapcore.ISO8601TimeEncoder
		jsonEnc := zapcore.NewJSONEncoder(esEncCfg)
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
	loggingCfg.Store(cfg)

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

	old := loggerPtr.Swap(&ErrnieLogger{Logger: z})
	if old != nil && old.Logger != nil {
		prev := old.Logger
		go func() {
			if syncErr := prev.Sync(); syncErr != nil {
				nl := loggerPtr.Load()
				if nl != nil && nl.Logger != nil {
					nl.Logger.Error("previous logger Sync failed", zap.Error(syncErr))
				} else {
					fmt.Fprintf(os.Stderr, "errnie: previous logger Sync failed: %v\n", syncErr)
				}
			}
		}()
	}

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
	l := loggerPtr.Load()
	if l == nil {
		return nil
	}
	z := l.Logger
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
	return loggerPtr.Load()
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
	logFile, err = os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
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
	l := loggerPtr.Load()
	if l == nil {
		return
	}
	z := l.Logger
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
	l := loggerPtr.Load()
	var z *zap.Logger
	if l != nil {
		z = l.Logger
	}
	debugEnabled := z != nil && z.Core().Enabled(zapcore.DebugLevel)

	cfg, _ := loggingCfg.Load().(LoggingConfig)
	tracePath := strings.TrimSpace(cfg.Trace.Path)
	traceEnabled := traceFile != nil || (tracePath != "" && filepath.Clean(tracePath) != os.DevNull)

	if !debugEnabled && !traceEnabled {
		return
	}

	formatted := traceKeyvalsFormatted(keyvals)
	if debugEnabled {
		base := keyvalsToFields(formatted)
		fields := make([]zap.Field, len(base)+1)
		copy(fields, base)
		fields[len(base)] = zap.String("component", "trace")
		z.Debug(msg, fields...)
	}
	if traceEnabled {
		ensureTraceFile()
		line := buildTraceLine(msg, formatted)
		if traceFile != nil {
			writeTraceLine(line)
			return
		}
	}
	fmt.Fprintln(os.Stderr, buildTraceLine(msg, formatted))
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
	l := loggerPtr.Load()
	var z *zap.Logger
	if l != nil {
		z = l.Logger
	}
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

	cfg, _ := loggingCfg.Load().(LoggingConfig)
	cfgPath := strings.TrimSpace(cfg.Trace.Path)

	path := cfgPath
	if path == "" {
		path = filepath.Join(wd, "trace.log")
	}
	if filepath.Clean(path) == os.DevNull {
		return
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
	traceQueue = make(chan string, 10000)
	go func() {
		for line := range traceQueue {
			if _, err := traceFile.WriteString(line); err != nil {
				fmt.Fprintf(os.Stderr, "errnie trace: write: %v\n", err)
			}
		}
	}()

	Debug("Trace log file initialized", "path", path)
}

func writeToLog(msg ...any) {
	if logFile == nil {
		return
	}
	appendLineToFile(logFile, msg)
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
	if traceFile == nil || traceQueue == nil {
		return
	}

	if line == "" {
		line = "\n"
	} else {
		line += "\n"
	}

	select {
	case traceQueue <- line:
	default:
		fmt.Fprintf(os.Stderr, "errnie trace: dropped line (buffer full)\n")
	}
}

func appendLineToFile(f *os.File, parts []any) {
	if len(parts) == 0 || f == nil {
		return
	}

	line := formatTraceLine(parts)
	if line == "" {
		line = "\n"
	} else {
		line += "\n"
	}

	_, err := f.WriteString(line)
	if err != nil {
		return
	}
}

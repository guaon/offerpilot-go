package logger

import (
	"encoding/json"
	"os"
	"time"
)

type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

var levelOrder = map[LogLevel]int{
	LogLevelDebug: 0,
	LogLevelInfo:  1,
	LogLevelWarn:  2,
	LogLevelError: 3,
}

type Logger struct {
	minLevel LogLevel
	context  map[string]interface{}
}

type LogEntry struct {
	Level   LogLevel               `json:"level"`
	Msg     string                 `json:"msg"`
	Ts      string                 `json:"ts"`
	Context map[string]interface{} `json:" context,omitempty"`
}

func NewLogger(opts ...Option) *Logger {
	l := &Logger{
		minLevel: LogLevelInfo,
		context:  make(map[string]interface{}),
	}
	if envLevel := os.Getenv("LOG_LEVEL"); envLevel != "" {
		l.minLevel = LogLevel(envLevel)
	}

	for _, opt := range opts {
		opt(l)
	}

	return l
}

type Option func(*Logger)

func WithLevel(level LogLevel) Option {
	return func(l *Logger) {
		l.minLevel = level
	}

}

func WithContext(ctx map[string]interface{}) Option {
	return func(l *Logger) {
		for k, v := range ctx {
			l.context[k] = v
		}
	}
}

func (l *Logger) Child(ctx map[string]interface{}) *Logger {
	newCtx := make(map[string]interface{})
	for k, v := range l.context {
		newCtx[k] = v
	}
	return &Logger{
		minLevel: l.minLevel,
		context:  newCtx,
	}
}

func (l *Logger) Debug(msg string, data ...map[string]interface{}) {
	l.log(LogLevelDebug, msg, data...)
}

func (l *Logger) Info(msg string, data ...map[string]interface{}) {
	l.log(LogLevelInfo, msg, data...)
}

func (l *Logger) Warn(msg string, data ...map[string]interface{}) {
	l.log(LogLevelWarn, msg, data...)
}

func (l *Logger) Error(msg string, data ...map[string]interface{}) {
	l.log(LogLevelError, msg, data...)
}

func (l *Logger) log(level LogLevel, msg string, data ...map[string]interface{}) {
	if levelOrder[level] < levelOrder[l.minLevel] {
		return
	}

	entry := LogEntry{
		Level:   level,
		Msg:     msg,
		Ts:      time.Now().UTC().Format(time.RFC3339),
		Context: make(map[string]interface{}),
	}

	for k, v := range l.context {
		entry.Context[k] = v
	}

	for _, d := range data {
		for k, v := range d {
			entry.Context[k] = v
		}
	}

	output, _ := json.Marshal(entry)

	if level == LogLevelError {
		os.Stderr.WriteString(string(output) + "\n")
	} else {
		os.Stdout.WriteString(string(output) + "\n")

	}
}

var DefaultLogger = NewLogger()

func Debug(msg string, data ...map[string]interface{}) {
	DefaultLogger.Debug(msg, data...)
}

func Info(msg string, data ...map[string]interface{}) {
	DefaultLogger.Info(msg, data...)
}

func Warn(msg string, data ...map[string]interface{}) {
	DefaultLogger.Warn(msg, data...)
}

func Error(msg string, data ...map[string]interface{}) {
	DefaultLogger.Error(msg, data...)
}

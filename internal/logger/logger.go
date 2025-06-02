package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LogLevel 日志级别
type LogLevel int

const (
	LogLevelError LogLevel = iota
	LogLevelWarn
	LogLevelInfo
	LogLevelDebug
	LogLevelTrace
)

// Logger 接口定义
type Logger interface {
	Error(format string, args ...interface{})
	Warn(format string, args ...interface{})
	Info(format string, args ...interface{})
	Debug(format string, args ...interface{})
	Trace(format string, args ...interface{})
	SetLevel(level LogLevel)
}

// DefaultLogger 默认日志实现
type DefaultLogger struct {
	level      LogLevel
	mu         sync.Mutex
	stdout     io.Writer
	debugFile  io.Writer
	debugMu    sync.Mutex
	debugLevel LogLevel
}

// NewDefaultLogger 创建默认日志记录器
func NewDefaultLogger() *DefaultLogger {
	return &DefaultLogger{
		level:      LogLevelInfo,
		stdout:     os.Stdout,
		debugLevel: LogLevelDebug,
	}
}

// SetDebugFile 设置调试日志文件
func (l *DefaultLogger) SetDebugFile(filename string) error {
	l.debugMu.Lock()
	defer l.debugMu.Unlock()

	if l.debugFile != nil {
		if closer, ok := l.debugFile.(io.Closer); ok {
			closer.Close()
		}
		l.debugFile = nil
	}

	if filename == "" {
		return nil
	}

	// 创建目录
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// 打开文件
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	l.debugFile = f
	return nil
}

// SetLevel 设置日志级别
func (l *DefaultLogger) SetLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// Error 输出错误日志
func (l *DefaultLogger) Error(format string, args ...interface{}) {
	l.log(LogLevelError, format, args...)
}

// Warn 输出警告日志
func (l *DefaultLogger) Warn(format string, args ...interface{}) {
	l.log(LogLevelWarn, format, args...)
}

// Info 输出信息日志
func (l *DefaultLogger) Info(format string, args ...interface{}) {
	l.log(LogLevelInfo, format, args...)
}

// Debug 输出调试日志
func (l *DefaultLogger) Debug(format string, args ...interface{}) {
	l.log(LogLevelDebug, format, args...)
}

// Trace 输出跟踪日志
func (l *DefaultLogger) Trace(format string, args ...interface{}) {
	l.log(LogLevelTrace, format, args...)
}

// log 内部日志记录方法
func (l *DefaultLogger) log(level LogLevel, format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 格式化消息
	msg := fmt.Sprintf(format, args...)
	now := time.Now().Format("2006-01-02 15:04:05")
	levelStr := getLevelString(level)

	// 输出到控制台
	if level <= l.level {
		fmt.Fprintf(l.stdout, "%s [%s] %s\n", now, levelStr, msg)
	}

	// 输出到调试文件
	l.debugMu.Lock()
	defer l.debugMu.Unlock()
	if l.debugFile != nil && level <= l.debugLevel {
		fmt.Fprintf(l.debugFile, "%s [%s] %s\n", now, levelStr, msg)
	}
}

// getLevelString 获取日志级别字符串
func getLevelString(level LogLevel) string {
	switch level {
	case LogLevelError:
		return "ERROR"
	case LogLevelWarn:
		return "WARN"
	case LogLevelInfo:
		return "INFO"
	case LogLevelDebug:
		return "DEBUG"
	case LogLevelTrace:
		return "TRACE"
	default:
		return "UNKNOWN"
	}
}

// Global 全局日志记录器
var Global Logger = NewDefaultLogger()

// SetGlobalLogger 设置全局日志记录器
func SetGlobalLogger(logger Logger) {
	Global = logger
}

// Error 全局错误日志
func Error(format string, args ...interface{}) {
	Global.Error(format, args...)
}

// Warn 全局警告日志
func Warn(format string, args ...interface{}) {
	Global.Warn(format, args...)
}

// Info 全局信息日志
func Info(format string, args ...interface{}) {
	Global.Info(format, args...)
}

// Debug 全局调试日志
func Debug(format string, args ...interface{}) {
	Global.Debug(format, args...)
}

// Trace 全局跟踪日志
func Trace(format string, args ...interface{}) {
	Global.Trace(format, args...)
}

// SetLevel 设置全局日志级别
func SetLevel(level LogLevel) {
	Global.SetLevel(level)
}

package logger

import (
	"io"
	"log"
	"os"
)

var (
	// 不同级别的日志记录器
	Debug   *log.Logger
	Info    *log.Logger
	Warning *log.Logger
	Error   *log.Logger
	
	// 是否启用调试日志
	debugEnabled bool
)

// Init 初始化日志系统
func Init() {
	debugEnabled = os.Getenv("REPO_DEBUG") == "1"
	
	// 设置输出
	debugOutput := io.Discard
	if debugEnabled {
		debugOutput = os.Stdout
	}
	
	// 创建日志记录器
	Debug = log.New(debugOutput, "DEBUG: ", log.Ldate|log.Ltime|log.Lshortfile)
	Info = log.New(os.Stdout, "INFO: ", log.Ldate|log.Ltime)
	Warning = log.New(os.Stdout, "WARNING: ", log.Ldate|log.Ltime)
	Error = log.New(os.Stderr, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)
}

// SetDebug 设置是否启用调试日志
func SetDebug(enabled bool) {
	debugEnabled = enabled
	if enabled {
		Debug.SetOutput(os.Stdout)
	} else {
		Debug.SetOutput(io.Discard)
	}
}

// Debugf 输出调试日志
func Debugf(format string, v ...interface{}) {
	Debug.Printf(format, v...)
}

// Infof 输出信息日志
func Infof(format string, v ...interface{}) {
	Info.Printf(format, v...)
}

// Warningf 输出警告日志
func Warningf(format string, v ...interface{}) {
	Warning.Printf(format, v...)
}

// Errorf 输出错误日志
func Errorf(format string, v ...interface{}) {
	Error.Printf(format, v...)
}

// Fatal 输出致命错误并退出
func Fatal(v ...interface{}) {
	Error.Fatal(v...)
}

// Fatalf 输出致命错误并退出
func Fatalf(format string, v ...interface{}) {
	Error.Fatalf(format, v...)
}
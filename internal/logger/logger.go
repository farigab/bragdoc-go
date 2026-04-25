// Package logger provides a very small, dependency-free logging facade
// used across the application for simple info/debug/error output.
package logger

import (
	"fmt"
	"log"
	"os"
	"strings"
)

// level constants — lower value = more verbose.
const (
	levelDebug = iota
	levelInfo
	levelError
)

var (
	std          = log.New(os.Stdout, "", log.LstdFlags|log.Lmicroseconds)
	currentLevel = levelInfo // default: info
)

// Init sets the minimum log level. Accepted values: "debug", "info", "error".
func Init(level string) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		currentLevel = levelDebug
	case "error":
		currentLevel = levelError
	default:
		currentLevel = levelInfo
	}
}

func formatKV(msg string, kv ...interface{}) string {
	if len(kv) == 0 {
		return msg
	}
	parts := make([]string, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		parts = append(parts, fmt.Sprintf("%v=%v", kv[i], kv[i+1]))
	}
	if len(kv)%2 == 1 {
		parts = append(parts, fmt.Sprintf("%v", kv[len(kv)-1]))
	}
	return fmt.Sprintf("%s %s", msg, strings.Join(parts, " "))
}

// Infof formats and logs an informational message.
func Infof(format string, args ...interface{}) {
	if currentLevel <= levelInfo {
		std.Printf("INFO "+format+"\n", args...)
	}
}

// Infow logs an informational message with key/value pairs.
func Infow(msg string, kv ...interface{}) {
	if currentLevel <= levelInfo {
		std.Printf("INFO %s\n", formatKV(msg, kv...))
	}
}

// Errorf formats and logs an error message.
func Errorf(format string, args ...interface{}) {
	if currentLevel <= levelError {
		std.Printf("ERROR "+format+"\n", args...)
	}
}

// Errorw logs an error message with key/value pairs.
func Errorw(msg string, kv ...interface{}) {
	if currentLevel <= levelError {
		std.Printf("ERROR %s\n", formatKV(msg, kv...))
	}
}

// Debugf formats and logs a debug message. No-op unless level is "debug".
func Debugf(format string, args ...interface{}) {
	if currentLevel <= levelDebug {
		std.Printf("DEBUG "+format+"\n", args...)
	}
}

// Debugw logs a debug message with key/value pairs. No-op unless level is "debug".
func Debugw(msg string, kv ...interface{}) {
	if currentLevel <= levelDebug {
		std.Printf("DEBUG %s\n", formatKV(msg, kv...))
	}
}

// Sync is a no-op kept for API compatibility.
func Sync() error { return nil }

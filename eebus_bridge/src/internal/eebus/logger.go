package eebus

import (
	"log"
	"strings"
)

// log levels in increasing verbosity
const (
	levelError = iota
	levelInfo
	levelDebug
	levelTrace
)

// Logger adapts the standard library logger to the ship-go logging interface
// with a configurable level.
type Logger struct {
	level int
}

// NewLogger creates a Logger from a textual level (error|info|debug|trace).
func NewLogger(level string) *Logger {
	return &Logger{level: parseLevel(level)}
}

func parseLevel(level string) int {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "error":
		return levelError
	case "debug":
		return levelDebug
	case "trace":
		return levelTrace
	default:
		return levelInfo
	}
}

func (l *Logger) logf(min int, prefix, format string, args ...interface{}) {
	if l.level >= min {
		log.Printf(prefix+format, args...)
	}
}

func (l *Logger) logln(min int, prefix string, args ...interface{}) {
	if l.level >= min {
		log.Print(append([]interface{}{prefix}, args...)...)
	}
}

func (l *Logger) Trace(args ...interface{}) { l.logln(levelTrace, "TRACE ", args...) }
func (l *Logger) Tracef(format string, args ...interface{}) {
	l.logf(levelTrace, "TRACE ", format, args...)
}
func (l *Logger) Debug(args ...interface{}) { l.logln(levelDebug, "DEBUG ", args...) }
func (l *Logger) Debugf(format string, args ...interface{}) {
	l.logf(levelDebug, "DEBUG ", format, args...)
}
func (l *Logger) Info(args ...interface{}) { l.logln(levelInfo, "INFO ", args...) }
func (l *Logger) Infof(format string, args ...interface{}) {
	l.logf(levelInfo, "INFO ", format, args...)
}
func (l *Logger) Error(args ...interface{}) { l.logln(levelError, "ERROR ", args...) }
func (l *Logger) Errorf(format string, args ...interface{}) {
	l.logf(levelError, "ERROR ", format, args...)
}

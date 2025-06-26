/*
Copyright 2019-2021 Intel Corporation

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package log

import (
	"context"
	"fmt"
	stdlog "log"
	"log/slog"
	"strings"
)

// Logger is the logging interface for goresctl
type Logger interface {
	Debugf(format string, v ...interface{})
	Infof(format string, v ...interface{})
	Warnf(format string, v ...interface{})
	Errorf(format string, v ...interface{})
	Panicf(format string, v ...interface{})
	Fatalf(format string, v ...interface{})
}

type logger struct {
	*stdlog.Logger
}

// NewLoggerWrapper wraps an implementation of the golang standard intreface
// into a goresctl specific compatible logger interface
func NewLoggerWrapper(l *stdlog.Logger) Logger {
	return &logger{Logger: l}
}

func (l *logger) Debugf(format string, v ...interface{}) {
	l.Printf("DEBUG: "+format, v...)
}

func (l *logger) Infof(format string, v ...interface{}) {
	l.Printf("INFO: "+format, v...)
}

func (l *logger) Warnf(format string, v ...interface{}) {
	l.Printf("WARN: "+format, v...)
}

func (l *logger) Errorf(format string, v ...interface{}) {
	l.Printf("ERROR: "+format, v...)
}

func (l *logger) Panicf(format string, v ...interface{}) {
	l.Logger.Panicf(format, v...)
}

func (l *logger) Fatalf(format string, v ...interface{}) {
	l.Logger.Fatalf(format, v...)
}

func InfoBlock(l Logger, heading, linePrefix, format string, v ...interface{}) {
	l.Infof("%s", heading)

	lines := strings.Split(fmt.Sprintf(format, v...), "\n")
	for _, line := range lines {
		l.Infof("%s%s", linePrefix, line)
	}
}

func DebugBlock(l Logger, heading, linePrefix, format string, v ...interface{}) {
	l.Debugf("%s", heading)

	lines := strings.Split(fmt.Sprintf(format, v...), "\n")
	for _, line := range lines {
		l.Debugf("%s%s", linePrefix, line)
	}
}

// Custom slog handler for changing the log level.
type logHandler struct {
	slog.Leveler
	slog.Handler
}

// NewLogHandler creates a new slog handler that uses the provided log level
// but otherwise clones the default slog handler.
func NewLogHandler(level slog.Leveler) slog.Handler {
	return &logHandler{
		Leveler: level,
		Handler: slog.Default().Handler(),
	}
}

// Enabled implements the slog.Handler interface.
func (h *logHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.Level()
}

// LevelFlag implement the flag.Value interface and can be used as a command
// line flag for specifying the log level.
type LevelFlag struct {
	level slog.Level
}

func NewLevelFlag(level slog.Level) *LevelFlag {
	return &LevelFlag{level: level}
}

// Set the log level.
func (l *LevelFlag) Set(s string) error {
	switch strings.ToLower(s) {
	case "debug":
		l.level = slog.LevelDebug
	case "info":
		l.level = slog.LevelInfo
	case "warn":
		l.level = slog.LevelWarn
	case "error":
		l.level = slog.LevelError
	default:
		return fmt.Errorf("must be one of: debug, info, warn, error")
	}
	return nil
}

// String returns the string representation of the log level.
func (l *LevelFlag) String() string {
	switch l.level {
	case slog.LevelDebug:
		return "debug"
	case slog.LevelInfo:
		return "info"
	case slog.LevelWarn:
		return "warn"
	case slog.LevelError:
		return "error"
	default:
		return fmt.Sprintf("level(%d)", l.level)
	}
}

func (l *LevelFlag) Level() slog.Level {
	return l.level
}

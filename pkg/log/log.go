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
	"fmt"
	stdlog "log"
	"strings"
)

// Logger is the logging interface for goresctl
type Logger interface {
	Debug(format string, v ...interface{})
	Info(format string, v ...interface{})
	Warn(format string, v ...interface{})
	Error(format string, v ...interface{})
	Panic(format string, v ...interface{})
	Fatal(format string, v ...interface{})
	DebugBlock(prefix, format string, v ...interface{})
	InfoBlock(prefix, format string, v ...interface{})
	Prefix() string
}

type logger struct {
	*stdlog.Logger
}

// NewLoggerWrapper wraps an implementation of the golang standard intreface
// into a goresctl specific compatible logger interface
func NewLoggerWrapper(l *stdlog.Logger) Logger {
	return &logger{Logger: l}
}

func (l *logger) Debug(format string, v ...interface{}) {
	l.Logger.Printf("DEBUG: "+format, v...)
}

func (l *logger) Info(format string, v ...interface{}) {
	l.Logger.Printf("INFO: "+format, v...)
}

func (l *logger) Warn(format string, v ...interface{}) {
	l.Logger.Printf("WARN: "+format, v...)
}

func (l *logger) Error(format string, v ...interface{}) {
	l.Logger.Printf("ERROR: "+format, v...)
}

func (l *logger) Panic(format string, v ...interface{}) {
	l.Logger.Panicf(format, v...)
}

func (l *logger) Fatal(format string, v ...interface{}) {
	l.Logger.Fatalf(format, v...)
}

func (l *logger) DebugBlock(prefix, format string, v ...interface{}) {
	l.blockPrint("DEBUG: ", prefix, format, v...)
}

func (l *logger) InfoBlock(prefix, format string, v ...interface{}) {
	l.blockPrint("INFO: ", prefix, format, v...)
}

func (l *logger) blockPrint(levelPrefix, linePrefix, format string, v ...interface{}) {
	msg := levelPrefix + linePrefix + fmt.Sprintf(format, v...)

	lines := strings.Split(msg, "\n")

	p := strings.Repeat(" ", len(l.Logger.Prefix())+len(levelPrefix)) + linePrefix
	l.Logger.Print(strings.Join(lines, "\n"+p))
}

func (l *logger) Prefix() string {
	return l.Logger.Prefix()
}

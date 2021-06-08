// Package log provides logging for lxcri.
package log

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rs/zerolog"
)

var TimeFormat = "15:04:05.000"

// zerlog log levels are mirrored for convenience.
const (
	TraceLevel = zerolog.TraceLevel
	DebugLevel = zerolog.DebugLevel
	InfoLevel  = zerolog.InfoLevel
	WarnLevel  = zerolog.WarnLevel
	ErrorLevel = zerolog.ErrorLevel
	FatalLevel = zerolog.FatalLevel
	PanicLevel = zerolog.PanicLevel
)

func init() {
	zerolog.LevelFieldName = "l"
	zerolog.MessageFieldName = "m"

	zerolog.TimestampFieldName = "t"
	zerolog.TimeFieldFormat = TimeFormat

	// liblxc timestamp format
	//zerolog.TimeFieldFormat = "20060102150405.000"

	zerolog.CallerFieldName = "c"
	zerolog.CallerMarshalFunc = func(file string, line int) string {
		return filepath.Base(file) + ":" + strconv.Itoa(line)
	}
}

// OpenFile opens a new or appends to an existing log file.
// The parent directory is created if it does not exist.
func OpenFile(name string, mode os.FileMode) (*os.File, error) {
	logDir := filepath.Dir(name)
	err := os.MkdirAll(logDir, 0750)
	if err != nil {
		return nil, fmt.Errorf("failed to create log file directory %s: %w", logDir, err)
	}
	return os.OpenFile(name, os.O_WRONLY|os.O_APPEND|os.O_CREATE, mode)
}

// ParseLevel is a wrapper for zerolog.ParseLevel
func ParseLevel(level string) (zerolog.Level, error) {
	return zerolog.ParseLevel(strings.ToLower(level))
}

// NewLogger creates a new zerlog.Context from the given arguments.
// The returned context is configured to log with timestamp and caller information.
func NewLogger(out io.Writer, level zerolog.Level) zerolog.Context {
	// NOTE Unfortunately it's not possible change the position of the timestamp.
	// The timestamp is appended to the to the log output because it is dynamically rendered
	// see https://github.com/rs/zerolog/issues/109
	return zerolog.New(out).Level(level).With().Timestamp().Caller()
}

// ConsoleLogger returns a new zerlog.Logger suited for console usage (e.g unit tests)
func ConsoleLogger(color bool, level zerolog.Level) zerolog.Context {
	return zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, NoColor: !color, TimeFormat: TimeFormat}).Level(level).With().Timestamp().Caller()
}

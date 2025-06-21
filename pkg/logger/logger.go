package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
)

type Level int

const (
	DEBUG Level = iota
	INFO
	NOTICE
	WARNING
	EMERGENCY
)

var levelNames = map[Level]string{
	DEBUG:     "DEBUG",
	INFO:      "INFO",
	NOTICE:    "NOTICE",
	WARNING:   "WARNING",
	EMERGENCY: "EMERGENCY",
}

var levelColors = map[Level]*color.Color{
	DEBUG:     color.New(color.FgCyan),
	INFO:      color.New(color.FgGreen),
	NOTICE:    color.New(color.FgBlue),
	WARNING:   color.New(color.FgYellow),
	EMERGENCY: color.New(color.FgRed, color.Bold),
}

type Logger struct {
	mu        sync.RWMutex
	level     Level
	output    io.Writer
	rotation  *Rotation
	useColors bool
	logger    *log.Logger
}

func New(level string, output string, rotation *RotationConfig, useColors bool) (*Logger, error) {
	l := &Logger{
		level:     parseLevel(level),
		useColors: useColors,
	}

	if output == "stdout" {
		l.output = os.Stdout
	} else {
		dir := filepath.Dir(output)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create log directory: %w", err)
		}

		rot, err := NewRotation(output, rotation)
		if err != nil {
			return nil, err
		}
		l.rotation = rot
		l.output = rot
	}

	l.logger = log.New(l.output, "", 0)
	return l, nil
}

func parseLevel(level string) Level {
	switch strings.ToUpper(level) {
	case "DEBUG":
		return DEBUG
	case "INFO":
		return INFO
	case "NOTICE":
		return NOTICE
	case "WARNING":
		return WARNING
	case "EMERGENCY":
		return EMERGENCY
	default:
		return INFO
	}
}

func (l *Logger) log(level Level, format string, args ...interface{}) {
	if level < l.level {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	levelName := levelNames[level]
	message := fmt.Sprintf(format, args...)

	if l.useColors && l.output == os.Stdout {
		colorFunc := levelColors[level]
		l.logger.Printf("[%s] %s %s", timestamp, colorFunc.Sprint(levelName), message)
	} else {
		l.logger.Printf("[%s] [%s] %s", timestamp, levelName, message)
	}

	if l.rotation != nil {
		l.rotation.Rotate()
	}
}

func (l *Logger) Debug(format string, args ...interface{}) {
	l.log(DEBUG, format, args...)
}

func (l *Logger) Info(format string, args ...interface{}) {
	l.log(INFO, format, args...)
}

func (l *Logger) Notice(format string, args ...interface{}) {
	l.log(NOTICE, format, args...)
}

func (l *Logger) Warning(format string, args ...interface{}) {
	l.log(WARNING, format, args...)
}

func (l *Logger) Emergency(format string, args ...interface{}) {
	l.log(EMERGENCY, format, args...)
}

func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

func (l *Logger) Close() error {
	if l.rotation != nil {
		return l.rotation.Close()
	}
	return nil
}

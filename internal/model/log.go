package model

import "errors"

type LogLevel uint8

const (
	DEBUG LogLevel = iota
	INFO
	WARNING
	ERROR
)

func (l *LogLevel) UnmarshalText(text []byte) error {
	switch string(text) {
	case "debug":
		*l = DEBUG
	case "info":
		*l = INFO
	case "warning":
		*l = WARNING
	case "error":
		*l = ERROR
	default:
		return errors.New("invalid log level")
	}
	return nil
}

func (l LogLevel) MarshalText() ([]byte, error) {
	switch l {
	case DEBUG:
		return []byte("debug"), nil
	case INFO:
		return []byte("info"), nil
	case WARNING:
		return []byte("warning"), nil
	case ERROR:
		return []byte("error"), nil
	default:
		return nil, errors.New("invalid log level")
	}
}

type LogMessage struct {
	Level   LogLevel `json:"level"`
	Message string   `json:"message"`
	Logger  string   `json:"logger"`
}

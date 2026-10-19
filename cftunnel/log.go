package cftunnel

import (
	"io"

	"github.com/rs/zerolog"
	"github.com/sirupsen/logrus"
)

// NewLogger builds the zerolog logger the embedded cloudflared writes to, at the level
// matching a logrus level, so one log level setting governs both ZAB's logrus output and
// cloudflared's zerolog output.
func NewLogger(w io.Writer, level logrus.Level) *zerolog.Logger {
	log := zerolog.New(w).With().Timestamp().Str("component", "cloudflared").Logger().Level(zerologLevel(level))
	return &log
}

func zerologLevel(level logrus.Level) zerolog.Level {
	switch level {
	case logrus.TraceLevel:
		return zerolog.TraceLevel
	case logrus.DebugLevel:
		return zerolog.DebugLevel
	case logrus.WarnLevel:
		return zerolog.WarnLevel
	case logrus.ErrorLevel, logrus.FatalLevel, logrus.PanicLevel:
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}

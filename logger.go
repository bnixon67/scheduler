// Copyright (c) 2024 Bill Nixon

package scheduler

import "log/slog"

var logger *slog.Logger = slog.Default()

// SetLogger allows users to set a custom logger for the scheduler package.
func SetLogger(l *slog.Logger) {
	if l != nil {
		logger = l
	}
}

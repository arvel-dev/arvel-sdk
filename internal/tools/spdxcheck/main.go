// SPDX-License-Identifier: Apache-2.0

// Command spdxcheck verifies that every .go file starts with the SPDX licence
// header required for its directory.
package main

import (
	"flag"
	"log/slog"
	"os"
)

// levelFromEnv maps ARVEL_LOG_LEVEL to a slog level. It lives here because no
// other tool needs it yet; it moves to logx in S2 if a second tool does.
func levelFromEnv(log *slog.Logger) slog.Level {
	switch v := os.Getenv("ARVEL_LOG_LEVEL"); v {
	case "", "info":
		return slog.LevelInfo
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		log.Warn("unknown ARVEL_LOG_LEVEL, using info", "value", v)
		return slog.LevelInfo
	}
}

func main() {
	root := flag.String("root", ".", "directory to scan")
	flag.Parse()

	bootstrap := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	level := levelFromEnv(bootstrap)
	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	checked, violations, err := Check(*root, log)
	if err != nil {
		log.Error("spdx check failed", "error", err)
		os.Exit(2)
	}
	for _, v := range violations {
		log.Error("missing or wrong SPDX header", "path", v.Path, "want", v.Want, "got", v.Got)
	}
	if len(violations) > 0 {
		os.Exit(1)
	}
	log.Info("spdx check passed", "files", checked)
}

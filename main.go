package main

import (
	"io"
	"log/slog"
	"os"
)

var logWriter io.Writer = os.Stdout
var logHandler = slog.NewJSONHandler(logWriter, nil)

func main() {
	log := slog.New(logHandler)
	log.Info("starting up pgcompare")
}

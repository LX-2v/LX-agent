package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"github.com/LX-2v/LX-agent/internal/server"
)

func main() {
	maxFile := flag.Int64("max-file-bytes", 64<<20, "maximum compressed or uncompressed input file size")
	maxDecoded := flag.Int64("max-decoded-bytes", 256<<20, "maximum decompressed profile size")
	storeBytes := flag.Int64("store-bytes", 64<<20, "maximum in-memory profile store size")
	phase0 := flag.Bool("phase0", false, "expose only the direct-file top tool spike")
	flag.Parse()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	s, err := server.New(server.Config{MaxFileBytes: *maxFile, MaxDecodedBytes: *maxDecoded, StoreBytes: *storeBytes, Phase0: *phase0}, logger)
	if err != nil {
		logger.Error("initialize server", "error", err)
		os.Exit(1)
	}
	if err := s.Run(context.Background()); err != nil {
		logger.Error("run server", "error", err)
		os.Exit(1)
	}
}

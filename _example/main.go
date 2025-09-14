package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"sync"

	"github.com/acquisitionist/teletron"
	"github.com/lmittmann/tint"
)

type config struct {
	token   string
	limiter struct {
		rps   float64
		burst int
	}
}

type application struct {
	bot    *teletron.Bot
	config config
	logger *slog.Logger
	wg     sync.WaitGroup
}

func main() {
	logger := slog.New(tint.NewHandler(os.Stdout, &tint.Options{Level: slog.LevelDebug}))

	err := run(logger)
	if err != nil {
		trace := string(debug.Stack())
		logger.Error(err.Error(), "trace", trace)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	var cfg config

	flag.StringVar(&cfg.token, "token", "TOKEN", "telegram token for the application")
	flag.Float64Var(&cfg.limiter.rps, "limiter-rps", 1.0, "rate limiter maximum requests per second per user")
	flag.IntVar(&cfg.limiter.burst, "limiter-burst", 5, "rate limiter maximum burst")

	showVersion := flag.Bool("version", false, "display version and exit")

	flag.Parse()

	if *showVersion {
		fmt.Printf("version: %s\n", Get())
		return nil
	}

	bot, err := teletron.NewBot(cfg.token, nil)
	if err != nil {
		panic("failed to create new bot: " + err.Error())
	}

	app := &application{
		bot:    bot,
		config: cfg,
		logger: logger,
	}

	return app.serveDSP()
}

func Get() string {
	bi, ok := debug.ReadBuildInfo()
	if ok {
		return bi.Main.Version
	}

	return "unavailable"
}

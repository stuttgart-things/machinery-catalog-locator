// Command catalog-locator startet den HTTP-Server.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/stuttgart-things/machinery-catalog-locator/internal/api"
	"github.com/stuttgart-things/machinery-catalog-locator/internal/catalog"
	"github.com/stuttgart-things/machinery-catalog-locator/internal/config"
	ghforge "github.com/stuttgart-things/machinery-catalog-locator/internal/github"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load()
	if err != nil {
		log.Error("konfiguration", "err", err)
		os.Exit(1)
	}

	client, tokenSource, err := ghforge.NewClient(cfg.GitHub)
	if err != nil {
		log.Error("github-client", "err", err)
		os.Exit(1)
	}

	reader := ghforge.NewReader(client)
	resolver := catalog.NewResolver(reader)
	prService := ghforge.NewPRService(client, tokenSource, cfg.Git)

	srv := &api.Server{
		Resolver: resolver,
		Reader:   reader,
		PR:       prService,
		Log:      log,
	}

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Info("server gestartet", "addr", cfg.ListenAddr, "app-auth", cfg.GitHub.UsesApp())
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("listen", "err", err)
			os.Exit(1)
		}
	}()

	// Graceful Shutdown.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Error("shutdown", "err", err)
	}
	log.Info("server beendet")
}

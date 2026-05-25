// Command machinery-catalog-locator starts the gRPC API on one port and
// the HTMX dashboard on another. Both share a single in-process
// CatalogService implementation so the dashboard and remote callers
// behave identically.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"

	"github.com/stuttgart-things/machinery-catalog-locator/catalogservice"
	"github.com/stuttgart-things/machinery-catalog-locator/internal/catalog"
	"github.com/stuttgart-things/machinery-catalog-locator/internal/config"
	ghforge "github.com/stuttgart-things/machinery-catalog-locator/internal/github"
	"github.com/stuttgart-things/machinery-catalog-locator/internal/grpcserver"
	"github.com/stuttgart-things/machinery-catalog-locator/internal/web"
)

// Build metadata. Overridden at link time via -ldflags="-X main.version=...".
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}

	client, tokenSource, err := ghforge.NewClient(cfg.GitHub)
	if err != nil {
		slog.Error("github client", "err", err)
		os.Exit(1)
	}

	reader := ghforge.NewReader(client)
	resolver := catalog.NewResolver(reader)
	prService := ghforge.NewPRService(client, tokenSource, cfg.Git)

	catalogSrv := &grpcserver.Server{
		Resolver: resolver,
		Reader:   reader,
		PR:       prService,
	}

	grpcLis, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.GRPCPort))
	if err != nil {
		slog.Error("listen grpc", "port", cfg.GRPCPort, "err", err)
		os.Exit(1)
	}

	// Keepalive keeps long-lived WatchTree streams alive through idle
	// timeouts on intermediaries.
	gs := grpc.NewServer(
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    2 * time.Minute,
			Timeout: 20 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             time.Minute,
			PermitWithoutStream: true,
		}),
	)
	catalogservice.RegisterCatalogServiceServer(gs, catalogSrv)

	healthSrv := health.NewServer()
	healthpb.RegisterHealthServer(gs, healthSrv)
	healthSrv.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	// Reflection lets grpcurl / Postman discover the service without
	// the .proto file on hand. Cheap to enable; standard for internal
	// services behind the umbrella tool.
	reflection.Register(gs)

	webSrv, err := web.New(catalogSrv, web.BuildInfo{Version: version, Commit: commit, Date: date})
	if err != nil {
		slog.Error("init web", "err", err)
		os.Exit(1)
	}
	httpSrv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:           webSrv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		slog.Info("http server listening", "port", cfg.HTTPPort)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http serve", "err", err)
		}
	}()

	go func() {
		slog.Info("grpc server listening", "port", cfg.GRPCPort, "app-auth", cfg.GitHub.UsesApp())
		if err := gs.Serve(grpcLis); err != nil {
			slog.Error("grpc serve", "err", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	slog.Info("shutdown", "signal", sig)

	healthSrv.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		slog.Error("http shutdown", "err", err)
	}
	gs.GracefulStop()
	slog.Info("stopped")
}

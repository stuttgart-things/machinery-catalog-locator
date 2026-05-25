// Command machinery-catalog-locator starts the gRPC API on one port and
// the HTMX dashboard on another. Both share a single in-process
// CatalogService implementation so the dashboard and remote callers
// behave identically.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
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

	localRoot := flag.String("local-root", "", "if set, serve from a local fixture directory instead of GitHub (PR-opening RPCs become Unimplemented)")
	flag.Parse()

	catalogSrv, grpcPort, httpPort, appAuth, err := buildServer(*localRoot)
	if err != nil {
		slog.Error("startup", "err", err)
		os.Exit(1)
	}

	grpcLis, err := net.Listen("tcp", fmt.Sprintf(":%d", grpcPort))
	if err != nil {
		slog.Error("listen grpc", "port", grpcPort, "err", err)
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
		Addr:              fmt.Sprintf(":%d", httpPort),
		Handler:           webSrv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		slog.Info("http server listening", "port", httpPort)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http serve", "err", err)
		}
	}()

	go func() {
		slog.Info("grpc server listening", "port", grpcPort, "app-auth", appAuth, "local-root", *localRoot)
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

// buildServer wires the grpcserver.Server depending on whether we're
// running in GitHub mode or against a local fixture directory.
//
// Local mode skips config.Load entirely (no GitHub credentials needed),
// uses catalog.LocalReader, and leaves Server.PR nil. The PR-opening
// RPCs (RemoveTarget, DeleteResource) detect the nil PR and return
// Unimplemented — read-only RPCs work normally.
func buildServer(localRoot string) (srv *grpcserver.Server, grpcPort, httpPort int, appAuth bool, err error) {
	if localRoot != "" {
		// Default StripPathPrefix to the local root so a realistic
		// GitHub URL whose path includes the same prefix
		// (e.g. .../blob/main/testdata/catalog/all-locations.yaml)
		// resolves to .../all-locations.yaml under the root instead of
		// doubling up.
		reader := catalog.LocalReader{
			Root:            localRoot,
			StripPathPrefix: strings.TrimSuffix(filepath.ToSlash(localRoot), "/") + "/",
		}
		return &grpcserver.Server{
			Resolver: catalog.NewResolver(reader),
			Reader:   reader,
		}, envInt("GRPC_PORT", 50051), envInt("HTTP_PORT", 8080), false, nil
	}

	cfg, err := config.Load()
	if err != nil {
		return nil, 0, 0, false, fmt.Errorf("config: %w", err)
	}
	client, tokenSource, err := ghforge.NewClient(cfg.GitHub)
	if err != nil {
		return nil, 0, 0, false, fmt.Errorf("github client: %w", err)
	}
	reader := ghforge.NewReader(client)
	return &grpcserver.Server{
		Resolver: catalog.NewResolver(reader),
		Reader:   reader,
		PR:       ghforge.NewPRService(client, tokenSource, cfg.Git),
	}, cfg.GRPCPort, cfg.HTTPPort, cfg.GitHub.UsesApp(), nil
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

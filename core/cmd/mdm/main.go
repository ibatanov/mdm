package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"mdm/core/internal/config"
	"mdm/core/internal/httpapi"
	"mdm/core/internal/infra"
	"mdm/core/internal/migrations"
	"mdm/core/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := infra.OpenPostgres(ctx, cfg.PostgresDSN)
	if err != nil {
		logger.Error("failed to connect to postgres", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := migrations.Run(ctx, db); err != nil {
		logger.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	kafkaChecker := infra.NewKafkaChecker(cfg.KafkaBrokers)
	dictionaries := store.NewDictionaryRepository(db)
	attributes := store.NewAttributeRepository(db)
	schemas := store.NewDictionarySchemaRepository(db)
	audit := store.NewAuditRepository(db)

	handler := httpapi.NewHandler(logger, db, kafkaChecker, dictionaries, attributes, schemas, audit)

	server := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
	}

	go func() {
		logger.Info("mdm api started",
			"port", cfg.HTTPPort,
			"postgres_dsn", cfg.PostgresDSN,
			"kafka_brokers", cfg.KafkaBrokers,
		)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server failed", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
}

package adminservice

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ride-hail/internal/admin"
	"ride-hail/internal/shared/db"
	"ride-hail/internal/shared/logging"
	"ride-hail/internal/shared/messaging"
)

func Run() {
	log := logging.New("admin-service")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info(ctx, "service_startup", "Starting admin-service...")

	pool, err := db.NewPool(ctx, postgresDSN())
	if err != nil {
		log.Error(ctx, "startup_failed", "failed to connect to postgres", err)
		os.Exit(1)
	}
	defer pool.Close()
	log.Info(ctx, "db_connected", "Successfully connected to PostgreSQL")

	rabbitCfg := messaging.Config{
		Host:     getEnv("RABBITMQ_HOST", "127.0.0.1"),
		Port:     getEnv("RABBITMQ_PORT", "5672"),
		User:     getEnv("RABBITMQ_USER", "guest"),
		Password: getEnv("RABBITMQ_PASSWORD", "guest"),
	}

	rabbitConn := messaging.NewConnection(rabbitCfg, log)
	go func() {
		if err := rabbitConn.Connect(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error(ctx, "rabbitmq_fatal", "RabbitMQ fatal connection error", err)
		}
	}()

	repo := admin.NewRepository(pool)
	handler := admin.NewHandler(repo, log)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	serverPort := getEnv("ADMIN_SERVICE_PORT", "3004")
	srv := &http.Server{
		Addr:         ":" + serverPort,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		log.Info(ctx, "server_started", "HTTP Server is listening", "port", serverPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error(ctx, "server_failed", "HTTP server failed to start", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	log.Info(context.Background(), "shutdown_started", "Received shutdown signal, stopping gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error(context.Background(), "shutdown_http_failed", "HTTP server graceful shutdown failed", err)
	}

	if err := rabbitConn.Close(); err != nil {
		log.Error(context.Background(), "shutdown_rabbitmq_failed", "RabbitMQ connection close failed", err)
	}

	log.Info(context.Background(), "shutdown_complete", "Admin service stopped successfully")
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func postgresDSN() string {
	return "postgres://" + getEnv("DB_USER", "ridehail_user") + ":" +
		getEnv("DB_PASSWORD", "ridehail_pass") + "@" +
		getEnv("DB_HOST", "127.0.0.1") + ":" +
		getEnv("DB_PORT", "5432") + "/" +
		getEnv("DB_NAME", "ridehail_db")
}

package driverlocationservice

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ride-hail/internal/driverlocation"
	"ride-hail/internal/shared/db"
	"ride-hail/internal/shared/logging"
	"ride-hail/internal/shared/messaging"
	"ride-hail/internal/shared/ws"
)

func Run() {
	log := logging.New("driver-location-service")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- База данных ---
	pool, err := db.NewPool(ctx, postgresDSN())
	if err != nil {
		log.Error(ctx, "startup_failed", "failed to connect to postgres", err)
		os.Exit(1)
	}
	defer pool.Close()

	// --- RabbitMQ ---
	rabbitCfg := messaging.Config{
		Host:     getEnv("RABBITMQ_HOST", "localhost"),
		Port:     getEnv("RABBITMQ_PORT", "5672"),
		User:     getEnv("RABBITMQ_USER", "guest"),
		Password: getEnv("RABBITMQ_PASSWORD", "guest"),
	}
	rabbitConn := messaging.NewConnection(rabbitCfg, log)
	if err := rabbitConn.Connect(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Error(ctx, "startup_failed", "failed to connect to rabbitmq", err)
		os.Exit(1)
	}

	publisher := messaging.NewPublisher(rabbitConn)
	repo := driverlocation.NewRepository(pool)

	var matcher *driverlocation.Matcher
	var handler *driverlocation.Handler
	hub := ws.NewHub(log, func(msg ws.InboundMessage) {
		if matcher == nil || handler == nil {
			return
		}
		driverlocation.NewWSMessageRouter(matcher, handler).Handle(msg)
	})
	matcher = driverlocation.NewMatcher(hub, repo, publisher, log)
	handler = driverlocation.NewHandler(repo, hub, publisher, log)

	service := driverlocation.NewService(matcher, repo, hub, publisher, log)

	service.StartConsuming(ctx, rabbitConn)

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/drivers/", handler.HandleDriverWS)
	mux.HandleFunc("/drivers/", routeDriverEndpoints(handler))

	serverPort := getEnv("DRIVER_LOCATION_SERVICE_PORT", "3001")
	srv := &http.Server{Addr: ":" + serverPort, Handler: mux}

	go func() {
		log.Info(ctx, "server_started", "driver-location-service listening", "port", serverPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error(ctx, "server_failed", "http server stopped unexpectedly", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	log.Info(context.Background(), "shutdown_started", "received shutdown signal")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error(context.Background(), "shutdown_http_failed", "http server graceful shutdown failed", err)
	}
	if err := rabbitConn.Close(); err != nil {
		log.Error(context.Background(), "shutdown_rabbitmq_failed", "rabbitmq connection close failed", err)
	}

	log.Info(context.Background(), "shutdown_complete", "driver-location-service stopped gracefully")
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func postgresDSN() string {
	host := getEnv("DB_HOST", "localhost")
	port := getEnv("DB_PORT", "5432")
	user := getEnv("DB_USER", "ridehail_user")
	pass := getEnv("DB_PASSWORD", "ridehail_pass")
	name := getEnv("DB_NAME", "ridehail_db")
	return "postgres://" + user + ":" + pass + "@" + host + ":" + port + "/" + name
}

func routeDriverEndpoints(h *driverlocation.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case hasSuffix(r.URL.Path, "/online"):
			h.HandleOnline(w, r)
		case hasSuffix(r.URL.Path, "/offline"):
			h.HandleOffline(w, r)
		case hasSuffix(r.URL.Path, "/start"):
			h.HandleStart(w, r)
		case hasSuffix(r.URL.Path, "/complete"):
			h.HandleComplete(w, r)
		case hasSuffix(r.URL.Path, "/location"):
			h.HandleLocation(w, r)
		default:
			http.NotFound(w, r)
		}
	}
}

func hasSuffix(s, suffix string) bool {
	if len(s) < len(suffix) {
		return false
	}
	return s[len(s)-len(suffix):] == suffix
}

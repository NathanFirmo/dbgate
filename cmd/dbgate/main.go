package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"dbgate/internal/config"
	"dbgate/internal/executor/mongo"
	"dbgate/internal/executor/mysql"
	"dbgate/internal/handler"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	mysqlExecutor, err := mysql.New(cfg.Databases)
	if err != nil {
		log.Fatalf("init mysql executor: %v", err)
	}

	mongoExecutor, err := mongo.New(cfg.Databases)
	if err != nil {
		mysqlExecutor.Close()
		log.Fatalf("init mongo executor: %v", err)
	}

	server := &http.Server{
		Addr:              cfg.ListenAddress(),
		Handler:           handler.New(mysqlExecutor, mongoExecutor),
		ReadHeaderTimeout: 2 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	shutdownCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		log.Printf("dbgate listening on %s", cfg.ListenAddress())
		serverErr <- server.ListenAndServe()
	}()

	exitCode := 0
	select {
	case <-shutdownCtx.Done():
		log.Printf("shutdown signal received")
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Printf("http server failed: %v", err)
			exitCode = 1
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("http shutdown error: %v", err)
	}
	if err := mongoExecutor.Close(ctx); err != nil {
		log.Printf("mongo shutdown error: %v", err)
	}
	if err := mysqlExecutor.Close(); err != nil {
		log.Printf("mysql shutdown error: %v", err)
	}

	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

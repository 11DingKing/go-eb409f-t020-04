package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"microgrid-ops/internal/api"
	"microgrid-ops/internal/dispatch"
	"microgrid-ops/internal/scheduler"
	"microgrid-ops/internal/store"
)

// Config holds all runtime configuration.
type Config struct {
	Port                  int           `json:"port"`
	StorePath             string        `json:"store_path"`
	MaintenanceTimeout    time.Duration `json:"maintenance_timeout"`
	AnomalyReportDeadline time.Duration `json:"anomaly_report_deadline"`
	SchedulerInterval     time.Duration `json:"scheduler_interval"`
}

func defaultConfig() Config {
	return Config{
		Port:                  58295,
		StorePath:             "data/state.json",
		MaintenanceTimeout:    5 * time.Minute,
		AnomalyReportDeadline: 10 * time.Minute,
		SchedulerInterval:     2 * time.Second,
	}
}

func loadConfig(path string) Config {
	cfg := defaultConfig()
	if path == "" {
		path = "config.json"
	}
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &cfg)
	}
	// Environment overrides.
	if v := os.Getenv("PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.Port = p
		}
	}
	if v := os.Getenv("STORE_PATH"); v != "" {
		cfg.StorePath = v
	}
	if v := os.Getenv("MAINTENANCE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.MaintenanceTimeout = d
		}
	}
	if v := os.Getenv("ANOMALY_REPORT_DEADLINE"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.AnomalyReportDeadline = d
		}
	}
	if cfg.Port == 0 {
		cfg.Port = 58295
	}
	return cfg
}

func main() {
	configPath := flag.String("config", "config.json", "path to config file")
	flag.Parse()

	cfg := loadConfig(*configPath)

	st := store.New(cfg.StorePath)
	orch := dispatch.NewOrchestrator(dispatch.Config{
		AnomalyReportDeadline: cfg.AnomalyReportDeadline,
		MaintenanceTimeout:    cfg.MaintenanceTimeout,
	}, st)

	if err := orch.Restore(); err != nil {
		log.Printf("warning: failed to restore state: %v", err)
	}

	sched := scheduler.New(orch, cfg.SchedulerInterval)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sched.Start(ctx); err != nil {
		log.Fatalf("failed to start scheduler: %v", err)
	}

	server := api.NewServer(orch, sched)
	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      server.Handler(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("shutting down...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		_ = httpServer.Shutdown(shutdownCtx)
		sched.Stop()
		cancel()
	}()

	log.Printf("microgrid-ops listening on :%d", cfg.Port)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/CloudNativeWorks/clustereye-agent-clickhouse/internal/agent"
	"github.com/CloudNativeWorks/clustereye-agent-clickhouse/internal/config"
	"github.com/CloudNativeWorks/clustereye-agent-clickhouse/internal/logger"
)

var (
	version   = "1.0.0"
	buildTime = "unknown"
	gitCommit = "unknown"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "", "Path to configuration file (default: auto-detect)")
	showVersion := flag.Bool("version", false, "Show version information")
	flag.Parse()

	// Show version
	if *showVersion {
		fmt.Printf("ClusterEye Agent for ClickHouse\n")
		fmt.Printf("Version: %s\n", version)
		fmt.Printf("Build Time: %s\n", buildTime)
		fmt.Printf("Git Commit: %s\n", gitCommit)
		os.Exit(0)
	}

	// Load configuration
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	if err := logger.Initialize(cfg.GetLogLevel(), cfg.Logging.UseFileLog, cfg.Logging.UseEventLog); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Close()

	logger.Info("=================================================")
	logger.Info("ClusterEye Agent for ClickHouse v%s", version)
	logger.Info("=================================================")
	logger.Info("Build Time: %s", buildTime)
	logger.Info("Git Commit: %s", gitCommit)
	logger.Info("Configuration loaded successfully")

	// Create and start agent
	ag := agent.NewAgent(cfg)
	if err := ag.Start(); err != nil {
		logger.Fatal("Failed to start agent: %v", err)
	}

	// Wait for agent to finish
	ag.Wait()
	logger.Info("Agent shutdown complete")
}

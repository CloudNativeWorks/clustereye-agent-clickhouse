package agent

import (
	"context"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/CloudNativeWorks/clustereye-agent-clickhouse/internal/collector"
	"github.com/CloudNativeWorks/clustereye-agent-clickhouse/internal/config"
	"github.com/CloudNativeWorks/clustereye-agent-clickhouse/internal/logger"
	"github.com/CloudNativeWorks/clustereye-agent-clickhouse/internal/reporter"
)

// Agent represents the main agent orchestrator
type Agent struct {
	cfg       *config.AgentConfig
	collector *collector.Collector
	reporter  *reporter.Reporter
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

// NewAgent creates a new agent instance
func NewAgent(cfg *config.AgentConfig) *Agent {
	ctx, cancel := context.WithCancel(context.Background())

	return &Agent{
		cfg:       cfg,
		collector: collector.NewCollector(cfg),
		reporter:  reporter.NewReporter(cfg),
		ctx:       ctx,
		cancel:    cancel,
	}
}

// Start starts the agent
func (a *Agent) Start() error {
	logger.Info("Starting ClusterEye Agent for ClickHouse...")
	logger.Info("Agent Key: %s", a.cfg.Key)
	logger.Info("Agent Name: %s", a.cfg.Name)

	// Start profiling if enabled
	if a.cfg.Profiling.Enabled {
		go a.startProfiling()
	}

	// Initialize collector
	logger.Info("Initializing ClickHouse collector...")
	a.collector.InitializeCollector()

	if !a.collector.IsHealthy() {
		logger.Warning("ClickHouse collector failed to initialize, will retry in background")
	}

	// Connect to gRPC server
	logger.Info("Connecting to ClusterEye server...")
	if err := a.reporter.Connect(); err != nil {
		logger.Warning("Failed to connect to server: %v", err)
		logger.Info("Will retry connection in background")
	} else {
		// Register agent
		if err := a.reporter.AgentRegistration("success", "clickhouse"); err != nil {
			logger.Warning("Failed to register agent: %v", err)
		}
	}

	// Set global reporter
	reporter.SetGlobalReporter(a.reporter)

	// Start background tasks
	a.wg.Add(3)
	go a.healthCheckLoop()
	go a.dataCollectionLoop()
	go a.heartbeatLoop()

	// Handle shutdown signals
	a.handleSignals()

	logger.Info("ClusterEye Agent started successfully")
	return nil
}

// Stop stops the agent
func (a *Agent) Stop() {
	logger.Info("Stopping ClusterEye Agent...")
	a.cancel()
	a.wg.Wait()
	a.reporter.Disconnect()
	logger.Info("ClusterEye Agent stopped")
}

// Wait waits for the agent to finish
func (a *Agent) Wait() {
	a.wg.Wait()
}

// healthCheckLoop performs periodic health checks
func (a *Agent) healthCheckLoop() {
	defer a.wg.Done()

	ticker := time.NewTicker(time.Duration(a.cfg.GRPC.HealthCheckIntervalSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.collector.PerformHealthCheck()
		}
	}
}

// dataCollectionLoop collects and sends data periodically
func (a *Agent) dataCollectionLoop() {
	defer a.wg.Done()

	// Use collection interval from config
	interval := time.Duration(a.cfg.Clickhouse.QueryMonitoring.CollectionIntervalSec) * time.Second
	if interval == 0 {
		interval = 60 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.collectAndSendData()
		}
	}
}

// collectAndSendData collects data and sends it to the server
func (a *Agent) collectAndSendData() {
	if !a.collector.IsHealthy() {
		logger.Debug("Skipping data collection: collector not healthy")
		return
	}

	logger.Debug("Collecting data...")
	data, err := a.collector.CollectAll()
	if err != nil {
		logger.Warning("Failed to collect data: %v", err)
		return
	}

	logger.Debug("Sending data to server...")
	if err := a.reporter.SendData(data); err != nil {
		logger.Warning("Failed to send data: %v", err)
	} else {
		logger.Debug("Data sent successfully")
	}
}

// heartbeatLoop sends periodic heartbeats
func (a *Agent) heartbeatLoop() {
	defer a.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			if err := a.reporter.SendHeartbeat(); err != nil {
				logger.Debug("Failed to send heartbeat: %v", err)
			}
		}
	}
}

// handleSignals handles OS signals for graceful shutdown
func (a *Agent) handleSignals() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Info("Received signal: %v", sig)
		a.Stop()
	}()
}

// startProfiling starts the profiling HTTP server
func (a *Agent) startProfiling() {
	addr := fmt.Sprintf("localhost:%d", a.cfg.Profiling.Port)
	logger.Info("Starting profiling server on %s", addr)
	logger.Info("Profiling endpoints available at http://%s/debug/pprof/", addr)

	server := &http.Server{
		Addr:              addr,
		ReadHeaderTimeout: 3 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Warning("Profiling server error: %v", err)
	}
}

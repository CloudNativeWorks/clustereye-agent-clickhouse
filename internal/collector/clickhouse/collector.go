package clickhouse

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/CloudNativeWorks/clustereye-agent-clickhouse/internal/config"
	"github.com/CloudNativeWorks/clustereye-agent-clickhouse/internal/logger"
	"github.com/CloudNativeWorks/clustereye-agent-clickhouse/internal/model"
)

// ClickhouseCollector manages ClickHouse monitoring and data collection
type ClickhouseCollector struct {
	cfg                *config.AgentConfig
	conn               driver.Conn
	lastCollectionTime time.Time
	collectionInterval time.Duration
	maxRetries         int
	backoffDuration    time.Duration
	isHealthy          bool
	lastHealthCheck    time.Time
	healthCheckMu      sync.RWMutex
	connMu             sync.Mutex
}

var (
	globalCollector     *ClickhouseCollector
	collectorMutex      sync.RWMutex
	startupRecoveryOnce sync.Once
)

// NewClickhouseCollector creates a new ClickHouse collector instance
func NewClickhouseCollector(cfg *config.AgentConfig) *ClickhouseCollector {
	return &ClickhouseCollector{
		cfg:                cfg,
		collectionInterval: time.Duration(cfg.Clickhouse.QueryMonitoring.CollectionIntervalSec) * time.Second,
		maxRetries:         3,
		backoffDuration:    5 * time.Second,
		isHealthy:          false,
	}
}

// EnsureDefaultCollector ensures the global collector is initialized
func EnsureDefaultCollector(cfg *config.AgentConfig) {
	collectorMutex.Lock()
	defer collectorMutex.Unlock()

	if globalCollector == nil {
		globalCollector = NewClickhouseCollector(cfg)
	}
}

// GetDefaultCollector returns the global collector instance
func GetDefaultCollector() *ClickhouseCollector {
	collectorMutex.RLock()
	defer collectorMutex.RUnlock()
	return globalCollector
}

// UpdateDefaultCollector updates the global collector with new configuration
func UpdateDefaultCollector(cfg *config.AgentConfig) {
	collectorMutex.Lock()
	defer collectorMutex.Unlock()

	if globalCollector != nil {
		globalCollector.Close()
	}
	globalCollector = NewClickhouseCollector(cfg)
}

// GetConnection returns a connection to ClickHouse
func (c *ClickhouseCollector) GetConnection() (driver.Conn, error) {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	// Return existing connection if valid
	if c.conn != nil {
		if err := c.conn.Ping(context.Background()); err == nil {
			return c.conn, nil
		}
		// Connection is stale, close it
		c.conn.Close()
		c.conn = nil
	}

	// Create new connection
	conn, err := c.connect()
	if err != nil {
		return nil, err
	}

	c.conn = conn
	return c.conn, nil
}

// connect establishes a new connection to ClickHouse
func (c *ClickhouseCollector) connect() (driver.Conn, error) {
	options := &clickhouse.Options{
		Addr: []string{fmt.Sprintf("%s:%s", c.cfg.Clickhouse.Host, c.cfg.Clickhouse.Port)},
		Auth: clickhouse.Auth{
			Database: c.cfg.Clickhouse.Database,
			Username: c.cfg.Clickhouse.User,
			Password: c.cfg.Clickhouse.Pass,
		},
		DialTimeout:      10 * time.Second,
		MaxOpenConns:     10,
		MaxIdleConns:     5,
		ConnMaxLifetime:  time.Hour,
		ConnOpenStrategy: clickhouse.ConnOpenInOrder,
	}

	// Configure TLS if enabled
	if c.cfg.Clickhouse.TLSEnabled {
		options.TLS = &tls.Config{
			InsecureSkipVerify: false,
		}
	}

	conn, err := clickhouse.Open(options)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to ClickHouse: %w", err)
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := conn.Ping(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to ping ClickHouse: %w", err)
	}

	return conn, nil
}

// Close closes the collector and releases resources
func (c *ClickhouseCollector) Close() {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

// IsHealthy returns the current health status
func (c *ClickhouseCollector) IsHealthy() bool {
	c.healthCheckMu.RLock()
	defer c.healthCheckMu.RUnlock()
	return c.isHealthy
}

// checkHealth performs a health check
func (c *ClickhouseCollector) checkHealth() bool {
	conn, err := c.GetConnection()
	if err != nil {
		logger.Warning("Health check failed: %v", err)
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := conn.Ping(ctx); err != nil {
		logger.Warning("Ping failed during health check: %v", err)
		return false
	}

	return true
}

// ForceHealthCheck performs an immediate health check and updates status
func (c *ClickhouseCollector) ForceHealthCheck() {
	healthy := c.checkHealth()

	c.healthCheckMu.Lock()
	c.isHealthy = healthy
	c.lastHealthCheck = time.Now()
	c.healthCheckMu.Unlock()

	if healthy {
		logger.Debug("Health check passed")
	} else {
		logger.Warning("Health check failed")
	}
}

// StartupRecovery attempts to establish connection during startup
func (c *ClickhouseCollector) StartupRecovery() {
	startupRecoveryOnce.Do(func() {
		logger.Info("Starting ClickHouse collector initialization...")

		for i := 0; i < c.maxRetries; i++ {
			if i > 0 {
				logger.Info("Retry attempt %d/%d after %v", i+1, c.maxRetries, c.backoffDuration)
				time.Sleep(c.backoffDuration)
			}

			conn, err := c.GetConnection()
			if err != nil {
				logger.Warning("Connection attempt %d failed: %v", i+1, err)
				continue
			}

			// Test query
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			var version string
			err = conn.QueryRow(ctx, "SELECT version()").Scan(&version)
			cancel()

			if err != nil {
				logger.Warning("Test query failed: %v", err)
				continue
			}

			logger.Info("Successfully connected to ClickHouse version: %s", version)
			c.healthCheckMu.Lock()
			c.isHealthy = true
			c.healthCheckMu.Unlock()
			return
		}

		logger.Error("Failed to connect to ClickHouse after %d attempts", c.maxRetries)
	})
}

// CanCollect checks if collection can proceed
func (c *ClickhouseCollector) CanCollect() bool {
	return c.IsHealthy()
}

// ShouldSkipCollection checks if this collection cycle should be skipped
func (c *ClickhouseCollector) ShouldSkipCollection() bool {
	if time.Since(c.lastCollectionTime) < c.collectionInterval {
		return true
	}
	return false
}

// GetClickhouseInfo collects basic ClickHouse instance information
func (c *ClickhouseCollector) GetClickhouseInfo() (*model.ClickhouseInfo, error) {
	conn, err := c.GetConnection()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	info := &model.ClickhouseInfo{
		ClusterName:   c.cfg.Clickhouse.Cluster,
		Location:      c.cfg.Clickhouse.Location,
		Port:          c.cfg.Clickhouse.Port,
		Status:        "running",
		IP:            getLocalIP(),
		LastCheckTime: time.Now(),
	}

	// Get version
	var version string
	err = conn.QueryRow(ctx, "SELECT version()").Scan(&version)
	if err != nil {
		logger.Warning("Failed to get version: %v", err)
	} else {
		info.Version = version
	}

	// Get hostname
	var hostname string
	err = conn.QueryRow(ctx, "SELECT hostName()").Scan(&hostname)
	if err != nil {
		logger.Warning("Failed to get hostname: %v", err)
	} else {
		info.Hostname = hostname
	}

	// Get uptime
	var uptime uint32
	err = conn.QueryRow(ctx, "SELECT uptime()").Scan(&uptime)
	if err != nil {
		logger.Warning("Failed to get uptime: %v", err)
	} else {
		info.Uptime = int64(uptime)
	}

	// Get data path from system.disks table (more reliable)
	var dataPath string
	err = conn.QueryRow(ctx, "SELECT path FROM system.disks WHERE name = 'default' LIMIT 1").Scan(&dataPath)
	if err != nil {
		// Fallback: try to get from system.settings
		err = conn.QueryRow(ctx, "SELECT value FROM system.settings WHERE name = 'path'").Scan(&dataPath)
		if err != nil {
			logger.Debug("Failed to get data path: %v", err)
			// Use default path as last resort
			dataPath = "/var/lib/clickhouse/"
		}
	}
	info.DataPath = dataPath

	// Check if clustering is enabled
	if c.cfg.Clickhouse.Cluster != "" {
		var shardCount, replicaCount uint64
		err = conn.QueryRow(ctx,
			"SELECT uniq(shard_num), uniq(replica_num) FROM system.clusters WHERE cluster = ?",
			c.cfg.Clickhouse.Cluster).Scan(&shardCount, &replicaCount)

		if err != nil {
			logger.Debug("Failed to get cluster info: %v", err)
		} else {
			info.IsReplicated = replicaCount > 1
			info.ShardCount = int(shardCount)
			info.ReplicaCount = int(replicaCount)
		}
	}

	// Get CPU count from asynchronous_metrics (numeric value only)
	var cpuCount float64
	err = conn.QueryRow(ctx, "SELECT value FROM system.asynchronous_metrics WHERE metric IN ('OSUserTimeCPUCores', 'NumberOfLogicalCPUCores', 'NumberOfPhysicalCPUCores') AND value != '' LIMIT 1").Scan(&cpuCount)
	if err != nil {
		// Fallback: try from system.settings
		var cpuStr string
		err = conn.QueryRow(ctx, "SELECT value FROM system.settings WHERE name = 'max_threads' LIMIT 1").Scan(&cpuStr)
		if err != nil {
			logger.Debug("Failed to get CPU count: %v", err)
			info.TotalVCPU = 0
		} else {
			// Parse the CPU count from string (might be 'auto' or a number)
			if cpuStr == "auto" || cpuStr == "" {
				info.TotalVCPU = 0
			} else {
				fmt.Sscanf(cpuStr, "%d", &info.TotalVCPU)
			}
		}
	} else {
		info.TotalVCPU = int(cpuCount)
	}

	// Get total memory from asynchronous_metrics (numeric value only)
	var totalMemory float64
	err = conn.QueryRow(ctx, "SELECT value FROM system.asynchronous_metrics WHERE metric IN ('OSMemoryTotal', 'OSMemoryAvailable', 'MemoryTracking') AND value != '' LIMIT 1").Scan(&totalMemory)
	if err != nil {
		// Fallback: try from system.metrics
		var memBytes int64
		err = conn.QueryRow(ctx, "SELECT value FROM system.metrics WHERE metric = 'MemoryTracking' LIMIT 1").Scan(&memBytes)
		if err != nil {
			logger.Debug("Failed to get total memory: %v", err)
			info.TotalMemory = 0
		} else {
			info.TotalMemory = memBytes
		}
	} else {
		info.TotalMemory = int64(totalMemory)
	}

	info.NodeStatus = "active"
	return info, nil
}

// getLocalIP returns the local IP address
func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "unknown"
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}

	return "unknown"
}

// CollectData collects all ClickHouse data
func (c *ClickhouseCollector) CollectData() (*model.ClickhouseData, error) {
	if !c.CanCollect() {
		return nil, fmt.Errorf("collector is not healthy")
	}

	data := &model.ClickhouseData{}

	// Collect basic info
	info, err := c.GetClickhouseInfo()
	if err != nil {
		logger.Warning("Failed to collect info: %v", err)
	} else {
		data.Info = info
	}

	// Collect metrics
	metrics, err := c.CollectMetrics()
	if err != nil {
		logger.Warning("Failed to collect metrics: %v", err)
	} else {
		data.Metrics = metrics
	}

	// Collect queries if enabled
	if c.cfg.Clickhouse.QueryMonitoring.Enabled {
		queries, err := c.CollectQueries()
		if err != nil {
			logger.Warning("Failed to collect queries: %v", err)
		} else {
			data.Queries = queries
		}
	}

	// Collect table metrics
	tables, err := c.CollectTableMetrics()
	if err != nil {
		logger.Warning("Failed to collect table metrics: %v", err)
	} else {
		data.Tables = tables
	}

	// Collect replication status if enabled
	if c.cfg.Clickhouse.ClusterMonitoring.Enabled && c.cfg.Clickhouse.Cluster != "" {
		replication, err := c.CollectReplicationStatus()
		if err != nil {
			logger.Warning("Failed to collect replication status: %v", err)
		} else {
			data.Replication = replication
		}
	}

	// Collect system tables data
	systemData, err := c.CollectSystemTablesData()
	if err != nil {
		logger.Warning("Failed to collect system tables data: %v", err)
	} else {
		data.SystemTables = systemData
	}

	// Collect advanced ClickHouse-specific metrics
	// These are critical for performance monitoring and alarming

	// Merge metrics (CRITICAL - merge backlog = future problems)
	mergeMetrics, err := c.CollectMergeMetrics()
	if err != nil {
		logger.Warning("Failed to collect merge metrics: %v", err)
	} else {
		data.MergeMetrics = mergeMetrics
	}

	// Parts metrics (equivalent to PostgreSQL bloat)
	partsMetrics, err := c.CollectPartsMetrics()
	if err != nil {
		logger.Warning("Failed to collect parts metrics: %v", err)
	} else {
		data.PartsMetrics = partsMetrics
	}

	// Memory pressure metrics
	memoryPressure, err := c.CollectMemoryPressureMetrics()
	if err != nil {
		logger.Warning("Failed to collect memory pressure metrics: %v", err)
	} else {
		data.MemoryPressure = memoryPressure
	}

	// Query concurrency metrics
	queryConcurrency, err := c.CollectQueryConcurrencyMetrics()
	if err != nil {
		logger.Warning("Failed to collect query concurrency metrics: %v", err)
	} else {
		data.QueryConcurrency = queryConcurrency
	}

	// ClickHouse errors (critical signals)
	errors, err := c.CollectClickHouseErrors()
	if err != nil {
		logger.Warning("Failed to collect ClickHouse errors: %v", err)
	} else {
		data.Errors = errors
	}

	c.lastCollectionTime = time.Now()
	return data, nil
}

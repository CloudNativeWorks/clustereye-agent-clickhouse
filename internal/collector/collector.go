package collector

import (
	"fmt"
	"time"

	"github.com/CloudNativeWorks/clustereye-agent-clickhouse/internal/collector/clickhouse"
	"github.com/CloudNativeWorks/clustereye-agent-clickhouse/internal/config"
	"github.com/CloudNativeWorks/clustereye-agent-clickhouse/internal/logger"
	"github.com/CloudNativeWorks/clustereye-agent-clickhouse/internal/model"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
)

// Collector orchestrates all data collection activities
type Collector struct {
	cfg                *config.AgentConfig
	clickhouseCollector *clickhouse.ClickhouseCollector
}

// NewCollector creates a new collector instance
func NewCollector(cfg *config.AgentConfig) *Collector {
	clickhouse.EnsureDefaultCollector(cfg)

	return &Collector{
		cfg:                 cfg,
		clickhouseCollector: clickhouse.GetDefaultCollector(),
	}
}

// CollectAll collects all available data
func (c *Collector) CollectAll() (*model.SystemData, error) {
	data := &model.SystemData{
		AgentKey:   c.cfg.Key,
		AgentName:  c.cfg.Name,
		Timestamp:  currentTimeMillis(),
	}

	// Collect ClickHouse data
	if c.clickhouseCollector.IsHealthy() {
		clickhouseData, err := c.clickhouseCollector.CollectData()
		if err != nil {
			logger.Warning("Failed to collect ClickHouse data: %v", err)
		} else {
			data.Clickhouse = clickhouseData
		}
	}

	// Collect system metrics
	systemMetrics, err := c.CollectSystemMetrics()
	if err != nil {
		logger.Warning("Failed to collect system metrics: %v", err)
	} else {
		data.System = systemMetrics
	}

	return data, nil
}

// CollectSystemMetrics collects system-level metrics
func (c *Collector) CollectSystemMetrics() (*model.SystemMetrics, error) {
	metrics := &model.SystemMetrics{}

	// CPU metrics
	cpuPercents, err := cpu.Percent(0, false)
	if err == nil && len(cpuPercents) > 0 {
		metrics.CPUUsage = cpuPercents[0]
	}

	cpuCounts, err := cpu.Counts(true)
	if err == nil {
		metrics.CPUCores = int32(cpuCounts)
	}

	// Memory metrics
	memInfo, err := mem.VirtualMemory()
	if err == nil {
		metrics.MemoryUsage = memInfo.UsedPercent
		metrics.MemoryTotal = int64(memInfo.Total)
		metrics.MemoryAvailable = int64(memInfo.Available)
	}

	// Disk metrics (for data partition)
	dataPath := "/"
	if c.clickhouseCollector != nil {
		chData, err := c.clickhouseCollector.GetClickhouseInfo()
		if err == nil && chData.DataPath != "" {
			dataPath = chData.DataPath
		}
	}

	diskInfo, err := disk.Usage(dataPath)
	if err == nil {
		metrics.DiskUsage = diskInfo.UsedPercent
		metrics.DiskTotal = int64(diskInfo.Total)
		metrics.DiskFree = int64(diskInfo.Free)
	}

	// Load average (Unix-like systems)
	loadAvg, err := load.Avg()
	if err == nil {
		metrics.LoadAverage = []float64{loadAvg.Load1, loadAvg.Load5, loadAvg.Load15}
	}

	// Uptime
	uptime, err := host.Uptime()
	if err == nil {
		metrics.Uptime = int64(uptime)
	}

	return metrics, nil
}

// InitializeCollector initializes the ClickHouse collector
func (c *Collector) InitializeCollector() {
	logger.Info("Initializing ClickHouse collector...")
	c.clickhouseCollector.StartupRecovery()
}

// IsHealthy checks if the collector is healthy
func (c *Collector) IsHealthy() bool {
	return c.clickhouseCollector.IsHealthy()
}

// PerformHealthCheck performs health checks on all collectors
func (c *Collector) PerformHealthCheck() {
	c.clickhouseCollector.ForceHealthCheck()
}

// ExecuteQuery executes a query on ClickHouse
func (c *Collector) ExecuteQuery(query string) (interface{}, error) {
	if !c.clickhouseCollector.IsHealthy() {
		return nil, fmt.Errorf("ClickHouse collector is not healthy")
	}

	return c.clickhouseCollector.ExecuteQuery(query)
}

// currentTimeMillis returns current time in milliseconds
func currentTimeMillis() int64 {
	return currentTimeNano() / 1_000_000
}

// currentTimeNano returns current time in nanoseconds
func currentTimeNano() int64 {
	return timeNow().UnixNano()
}

// Mock-friendly time function
var timeNow = func() time.Time {
	return time.Now()
}

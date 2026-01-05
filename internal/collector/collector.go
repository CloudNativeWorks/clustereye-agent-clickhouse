package collector

import (
	"fmt"
	"strings"
	"sync"
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
	"github.com/shirou/gopsutil/v3/net"
)

// Collector orchestrates all data collection activities
type Collector struct {
	cfg                 *config.AgentConfig
	clickhouseCollector *clickhouse.ClickhouseCollector

	// Previous counters for rate calculation
	prevNetStats     map[string]net.IOCountersStat
	prevDiskStats    map[string]disk.IOCountersStat
	prevCollectTime  time.Time
	statsMu          sync.RWMutex
}

// NewCollector creates a new collector instance
func NewCollector(cfg *config.AgentConfig) *Collector {
	clickhouse.EnsureDefaultCollector(cfg)

	return &Collector{
		cfg:                 cfg,
		clickhouseCollector: clickhouse.GetDefaultCollector(),
		prevNetStats:        make(map[string]net.IOCountersStat),
		prevDiskStats:       make(map[string]disk.IOCountersStat),
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
	metrics := &model.SystemMetrics{
		Timestamp: time.Now(),
	}

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

	// Network metrics
	networkMetrics, err := c.collectNetworkMetrics()
	if err != nil {
		logger.Debug("Failed to collect network metrics: %v", err)
	} else {
		metrics.NetworkInterfaces = networkMetrics
	}

	// Disk I/O metrics
	diskIOMetrics, err := c.collectDiskIOMetrics()
	if err != nil {
		logger.Debug("Failed to collect disk I/O metrics: %v", err)
	} else {
		metrics.DiskIOMetrics = diskIOMetrics
	}

	return metrics, nil
}

// collectNetworkMetrics collects network interface metrics
func (c *Collector) collectNetworkMetrics() ([]model.NetworkInterfaceMetrics, error) {
	netStats, err := net.IOCounters(true) // true = per interface
	if err != nil {
		return nil, err
	}

	c.statsMu.Lock()
	defer c.statsMu.Unlock()

	now := time.Now()
	elapsed := now.Sub(c.prevCollectTime).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}

	result := make([]model.NetworkInterfaceMetrics, 0)

	for _, stat := range netStats {
		// Skip loopback and virtual interfaces
		if stat.Name == "lo" || strings.HasPrefix(stat.Name, "veth") ||
			strings.HasPrefix(stat.Name, "docker") || strings.HasPrefix(stat.Name, "br-") {
			continue
		}

		metric := model.NetworkInterfaceMetrics{
			Interface:       stat.Name,
			BytesSent:       stat.BytesSent,
			BytesReceived:   stat.BytesRecv,
			PacketsSent:     stat.PacketsSent,
			PacketsReceived: stat.PacketsRecv,
			ErrorsIn:        stat.Errin,
			ErrorsOut:       stat.Errout,
			DropsIn:         stat.Dropin,
			DropsOut:        stat.Dropout,
		}

		// Calculate throughput if we have previous data
		if prev, ok := c.prevNetStats[stat.Name]; ok && !c.prevCollectTime.IsZero() {
			bytesSentDiff := float64(stat.BytesSent - prev.BytesSent)
			bytesRecvDiff := float64(stat.BytesRecv - prev.BytesRecv)

			// Convert to Mbps (megabits per second)
			metric.ThroughputSentMbps = (bytesSentDiff * 8) / (elapsed * 1000000)
			metric.ThroughputRecvMbps = (bytesRecvDiff * 8) / (elapsed * 1000000)
		}

		// Update previous stats
		c.prevNetStats[stat.Name] = stat

		result = append(result, metric)
	}

	c.prevCollectTime = now
	return result, nil
}

// collectDiskIOMetrics collects disk I/O metrics
func (c *Collector) collectDiskIOMetrics() ([]model.DiskIOMetrics, error) {
	diskStats, err := disk.IOCounters()
	if err != nil {
		return nil, err
	}

	c.statsMu.Lock()
	defer c.statsMu.Unlock()

	now := time.Now()
	elapsed := now.Sub(c.prevCollectTime).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}

	result := make([]model.DiskIOMetrics, 0)

	for name, stat := range diskStats {
		// Skip partitions (only collect whole disk metrics)
		// e.g., sda1, sda2 -> skip, sda -> collect
		if len(name) > 3 && name[len(name)-1] >= '0' && name[len(name)-1] <= '9' {
			// Check if it's a partition (ends with number after letters)
			if name[len(name)-2] >= 'a' && name[len(name)-2] <= 'z' {
				continue
			}
		}

		metric := model.DiskIOMetrics{
			Disk:       name,
			ReadCount:  stat.ReadCount,
			WriteCount: stat.WriteCount,
			ReadBytes:  stat.ReadBytes,
			WriteBytes: stat.WriteBytes,
			ReadTime:   stat.ReadTime,
			WriteTime:  stat.WriteTime,
			IoTime:     stat.IoTime,
		}

		// Calculate IOPS and throughput if we have previous data
		if prev, ok := c.prevDiskStats[name]; ok && !c.prevCollectTime.IsZero() {
			readCountDiff := float64(stat.ReadCount - prev.ReadCount)
			writeCountDiff := float64(stat.WriteCount - prev.WriteCount)
			readBytesDiff := float64(stat.ReadBytes - prev.ReadBytes)
			writeBytesDiff := float64(stat.WriteBytes - prev.WriteBytes)

			metric.ReadIOPS = readCountDiff / elapsed
			metric.WriteIOPS = writeCountDiff / elapsed
			metric.ReadThroughputMBs = readBytesDiff / (elapsed * 1024 * 1024)
			metric.WriteThroughputMBs = writeBytesDiff / (elapsed * 1024 * 1024)
		}

		// Update previous stats
		c.prevDiskStats[name] = stat

		result = append(result, metric)
	}

	return result, nil
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

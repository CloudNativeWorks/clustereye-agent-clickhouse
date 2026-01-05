package alarm

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	pb "github.com/CloudNativeWorks/clustereye-api/pkg/agent"
	"github.com/CloudNativeWorks/clustereye-agent-clickhouse/internal/collector/clickhouse"
	"github.com/CloudNativeWorks/clustereye-agent-clickhouse/internal/config"
	"github.com/CloudNativeWorks/clustereye-agent-clickhouse/internal/logger"
)

const (
	// AgentVersion for ClickHouse agent
	AgentVersion = "1.0.0"
)

// AlarmMonitor monitors ClickHouse metrics and sends alarms
type AlarmMonitor struct {
	client                  pb.AgentServiceClient
	agentID                 string
	stopCh                  chan struct{}
	alarmCache              map[string]*pb.AlarmEvent
	alarmCacheLock          sync.RWMutex
	checkInterval           time.Duration
	config                  *config.AgentConfig
	thresholds              *pb.ThresholdSettings
	clickhouseCollector     *clickhouse.ClickhouseCollector
	clientRefreshCallback   func() (pb.AgentServiceClient, error)
	thresholdUpdateCallback func(*pb.ThresholdSettings)
}

// ClickHouse-specific thresholds (can be extended via config later)
type ClickHouseThresholds struct {
	// Parts thresholds
	MaxPartsPerTable      int64   // Default: 300 warning, 1000 critical
	SmallPartsRatioWarn   float64 // Default: 0.5

	// Merge thresholds
	MergeBacklogWarn      int64   // Default: 10
	MergeBacklogCritical  int64   // Default: 50
	MaxMergeElapsedWarn   float64 // Default: 300 seconds

	// Memory thresholds
	MemoryUsageWarn       float64 // Default: 70%
	MemoryUsageCritical   float64 // Default: 85%

	// Query thresholds
	LongRunningQueryWarn  float64 // Default: 60 seconds
	LongRunningQueryCrit  float64 // Default: 300 seconds
	MaxRunningQueries     int64   // Default: 50

	// Replication thresholds
	ReplicationDelayWarn  int64   // Default: 60 seconds
	ReplicationDelayCrit  int64   // Default: 300 seconds

	// Mutation thresholds
	MutationAgeWarn       float64 // Default: 3600 seconds (1 hour)
	MutationAgeCritical   float64 // Default: 86400 seconds (24 hours)
}

// DefaultClickHouseThresholds returns default threshold values
func DefaultClickHouseThresholds() *ClickHouseThresholds {
	return &ClickHouseThresholds{
		MaxPartsPerTable:      300,
		SmallPartsRatioWarn:   0.5,
		MergeBacklogWarn:      10,
		MergeBacklogCritical:  50,
		MaxMergeElapsedWarn:   300,
		MemoryUsageWarn:       70,
		MemoryUsageCritical:   85,
		LongRunningQueryWarn:  60,
		LongRunningQueryCrit:  300,
		MaxRunningQueries:     50,
		ReplicationDelayWarn:  60,
		ReplicationDelayCrit:  300,
		MutationAgeWarn:       3600,
		MutationAgeCritical:   86400,
	}
}

// NewAlarmMonitor creates a new alarm monitor for ClickHouse
func NewAlarmMonitor(client pb.AgentServiceClient, agentID string, cfg *config.AgentConfig, refreshCallback func() (pb.AgentServiceClient, error)) *AlarmMonitor {
	monitor := &AlarmMonitor{
		client:                client,
		agentID:               agentID,
		stopCh:                make(chan struct{}),
		alarmCache:            make(map[string]*pb.AlarmEvent),
		checkInterval:         30 * time.Second,
		config:                cfg,
		clickhouseCollector:   clickhouse.GetDefaultCollector(),
		clientRefreshCallback: refreshCallback,
	}

	// Get initial threshold settings
	monitor.updateThresholds()

	return monitor
}

// SetThresholdUpdateCallback sets callback for threshold updates
func (m *AlarmMonitor) SetThresholdUpdateCallback(callback func(*pb.ThresholdSettings)) {
	m.thresholdUpdateCallback = callback
}

// updateThresholds fetches threshold settings from API
func (m *AlarmMonitor) updateThresholds() {
	maxRetries := 3
	backoff := time.Second * 2

	for attempt := 0; attempt < maxRetries; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

		req := &pb.GetThresholdSettingsRequest{
			AgentId: m.agentID,
		}

		resp, err := m.client.GetThresholdSettings(ctx, req)
		cancel()

		if err != nil {
			if attempt < maxRetries-1 {
				logger.Warning("Failed to get threshold settings (attempt %d/%d): %v", attempt+1, maxRetries, err)
				time.Sleep(backoff * time.Duration(attempt+1))
				continue
			}
			logger.Warning("Using default thresholds, couldn't fetch from server: %v", err)
			return
		}

		m.thresholds = resp.Settings
		logger.Info("Threshold settings updated: CPU=%.2f%%, Memory=%.2f%%, Disk=%.2f%%",
			m.thresholds.CpuThreshold,
			m.thresholds.MemoryThreshold,
			m.thresholds.DiskThreshold,
		)

		if m.thresholdUpdateCallback != nil {
			m.thresholdUpdateCallback(m.thresholds)
		}
		return
	}
}

// Start starts the alarm monitoring loop
func (m *AlarmMonitor) Start() {
	go m.monitorLoop()
	go m.reportAgentVersion()
	logger.Info("ClickHouse Alarm Monitor started")
}

// Stop stops the alarm monitoring
func (m *AlarmMonitor) Stop() {
	close(m.stopCh)
	logger.Info("ClickHouse Alarm Monitor stopped")
}

// SetCheckInterval sets the alarm check interval
func (m *AlarmMonitor) SetCheckInterval(interval time.Duration) {
	m.checkInterval = interval
}

// monitorLoop periodically checks for alarm conditions
func (m *AlarmMonitor) monitorLoop() {
	ticker := time.NewTicker(m.checkInterval)
	thresholdUpdateTicker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	defer thresholdUpdateTicker.Stop()

	for {
		select {
		case <-ticker.C:
			logger.Debug("Running periodic alarm checks (interval: %v)", m.checkInterval)
			m.checkAlarms()
		case <-thresholdUpdateTicker.C:
			logger.Debug("Updating threshold settings...")
			m.updateThresholds()
		case <-m.stopCh:
			return
		}
	}
}

// checkAlarms checks all alarm conditions for ClickHouse
func (m *AlarmMonitor) checkAlarms() {
	if m.clickhouseCollector == nil || !m.clickhouseCollector.IsHealthy() {
		logger.Debug("ClickHouse collector not healthy, skipping alarm checks")
		return
	}

	// Check service status
	m.checkServiceStatus()

	// Check merge metrics (CRITICAL)
	m.checkMergeMetrics()

	// Check parts metrics (bloat equivalent)
	m.checkPartsMetrics()

	// Check memory pressure
	m.checkMemoryPressure()

	// Check query concurrency
	m.checkQueryConcurrency()

	// Check replication status
	m.checkReplicationStatus()

	// Check mutations
	m.checkMutations()

	// Check ClickHouse errors
	m.checkClickHouseErrors()

	// Check disk usage
	m.checkDiskUsage()
}

// checkServiceStatus checks if ClickHouse service is running
func (m *AlarmMonitor) checkServiceStatus() {
	alarmKey := "clickhouse_service_status"

	m.alarmCacheLock.RLock()
	prevAlarm, exists := m.alarmCache[alarmKey]
	m.alarmCacheLock.RUnlock()

	healthy := m.clickhouseCollector.IsHealthy()

	if !healthy {
		if exists && prevAlarm.Status == "triggered" {
			return // Already reported
		}

		event := &pb.AlarmEvent{
			Id:          uuid.New().String(),
			AlarmId:     alarmKey,
			AgentId:     m.agentID,
			Status:      "triggered",
			MetricName:  "clickhouse_service_status",
			MetricValue: "FAIL",
			Message:     "ClickHouse service is not responding",
			Timestamp:   time.Now().Format(time.RFC3339),
			Severity:    "critical",
		}

		if err := m.reportAlarm(event); err != nil {
			logger.Error("Failed to send service alarm: %v", err)
		} else {
			m.cacheAlarm(alarmKey, event)
			logger.Warning("ClickHouse service alarm triggered (ID: %s)", event.Id)
		}
	} else if exists && prevAlarm.Status == "triggered" {
		// Service recovered
		event := &pb.AlarmEvent{
			Id:          uuid.New().String(),
			AlarmId:     alarmKey,
			AgentId:     m.agentID,
			Status:      "resolved",
			MetricName:  "clickhouse_service_status",
			MetricValue: "RUNNING",
			Message:     "ClickHouse service is running again",
			Timestamp:   time.Now().Format(time.RFC3339),
			Severity:    "info",
		}

		if err := m.reportAlarm(event); err != nil {
			logger.Error("Failed to send service resolved alarm: %v", err)
		} else {
			m.cacheAlarm(alarmKey, event)
			logger.Info("ClickHouse service alarm resolved (ID: %s)", event.Id)
		}
	}
}

// checkMergeMetrics checks merge operation metrics
func (m *AlarmMonitor) checkMergeMetrics() {
	metrics, err := m.clickhouseCollector.CollectMergeMetrics()
	if err != nil || metrics == nil {
		return
	}

	thresholds := DefaultClickHouseThresholds()

	// Check merge backlog
	alarmKey := "clickhouse_merge_backlog"
	m.alarmCacheLock.RLock()
	prevAlarm, exists := m.alarmCache[alarmKey]
	m.alarmCacheLock.RUnlock()

	if metrics.MergeBacklog >= thresholds.MergeBacklogCritical {
		if !exists || prevAlarm.Status != "triggered" {
			event := &pb.AlarmEvent{
				Id:          uuid.New().String(),
				AlarmId:     alarmKey,
				AgentId:     m.agentID,
				Status:      "triggered",
				MetricName:  "clickhouse_merge_backlog",
				MetricValue: fmt.Sprintf("%d", metrics.MergeBacklog),
				Message:     fmt.Sprintf("Critical merge backlog: %d pending merges (threshold: %d). This may cause performance degradation and disk space issues.", metrics.MergeBacklog, thresholds.MergeBacklogCritical),
				Timestamp:   time.Now().Format(time.RFC3339),
				Severity:    "critical",
			}
			m.sendAndCacheAlarm(alarmKey, event)
		}
	} else if metrics.MergeBacklog >= thresholds.MergeBacklogWarn {
		if !exists || prevAlarm.Status != "triggered" {
			event := &pb.AlarmEvent{
				Id:          uuid.New().String(),
				AlarmId:     alarmKey,
				AgentId:     m.agentID,
				Status:      "triggered",
				MetricName:  "clickhouse_merge_backlog",
				MetricValue: fmt.Sprintf("%d", metrics.MergeBacklog),
				Message:     fmt.Sprintf("Merge backlog building up: %d pending merges (threshold: %d)", metrics.MergeBacklog, thresholds.MergeBacklogWarn),
				Timestamp:   time.Now().Format(time.RFC3339),
				Severity:    "warning",
			}
			m.sendAndCacheAlarm(alarmKey, event)
		}
	} else if exists && prevAlarm.Status == "triggered" {
		// Resolved
		event := &pb.AlarmEvent{
			Id:          uuid.New().String(),
			AlarmId:     alarmKey,
			AgentId:     m.agentID,
			Status:      "resolved",
			MetricName:  "clickhouse_merge_backlog",
			MetricValue: fmt.Sprintf("%d", metrics.MergeBacklog),
			Message:     fmt.Sprintf("Merge backlog normalized: %d pending merges", metrics.MergeBacklog),
			Timestamp:   time.Now().Format(time.RFC3339),
			Severity:    "info",
		}
		m.sendAndCacheAlarm(alarmKey, event)
	}

	// Check long-running merges
	if metrics.MaxMergeElapsed > thresholds.MaxMergeElapsedWarn {
		alarmKey := "clickhouse_long_merge"
		m.alarmCacheLock.RLock()
		prevAlarm, exists := m.alarmCache[alarmKey]
		m.alarmCacheLock.RUnlock()

		if !exists || prevAlarm.Status != "triggered" {
			event := &pb.AlarmEvent{
				Id:          uuid.New().String(),
				AlarmId:     alarmKey,
				AgentId:     m.agentID,
				Status:      "triggered",
				MetricName:  "clickhouse_long_merge",
				MetricValue: fmt.Sprintf("%.2f", metrics.MaxMergeElapsed),
				Message:     fmt.Sprintf("Long-running merge detected: %.2f seconds. Active merges: %d", metrics.MaxMergeElapsed, metrics.ActiveMerges),
				Timestamp:   time.Now().Format(time.RFC3339),
				Severity:    "warning",
			}
			m.sendAndCacheAlarm(alarmKey, event)
		}
	}
}

// checkPartsMetrics checks parts metrics (bloat equivalent)
func (m *AlarmMonitor) checkPartsMetrics() {
	metrics, err := m.clickhouseCollector.CollectPartsMetrics()
	if err != nil || metrics == nil {
		return
	}

	thresholds := DefaultClickHouseThresholds()

	// Check tables with too many parts
	if len(metrics.TablesWithTooManyParts) > 0 {
		alarmKey := "clickhouse_too_many_parts"
		m.alarmCacheLock.RLock()
		prevAlarm, exists := m.alarmCache[alarmKey]
		m.alarmCacheLock.RUnlock()

		if !exists || prevAlarm.Status != "triggered" {
			var tableInfo strings.Builder
			for i, t := range metrics.TablesWithTooManyParts {
				if i >= 5 {
					tableInfo.WriteString(fmt.Sprintf("... and %d more tables", len(metrics.TablesWithTooManyParts)-5))
					break
				}
				tableInfo.WriteString(fmt.Sprintf("%s.%s: %d parts, ", t.Database, t.Table, t.ActiveParts))
			}

			severity := "warning"
			if metrics.MaxPartsPerTable > 1000 {
				severity = "critical"
			}

			event := &pb.AlarmEvent{
				Id:          uuid.New().String(),
				AlarmId:     alarmKey,
				AgentId:     m.agentID,
				Status:      "triggered",
				MetricName:  "clickhouse_too_many_parts",
				MetricValue: fmt.Sprintf("%d", metrics.MaxPartsPerTable),
				Message:     fmt.Sprintf("Tables with too many parts detected (%d tables). Max parts: %d. Tables: %s. This indicates merge process is falling behind.", len(metrics.TablesWithTooManyParts), metrics.MaxPartsPerTable, tableInfo.String()),
				Timestamp:   time.Now().Format(time.RFC3339),
				Severity:    severity,
			}
			m.sendAndCacheAlarm(alarmKey, event)
		}
	}

	// Check small parts ratio
	if metrics.SmallPartsRatio > thresholds.SmallPartsRatioWarn && metrics.ActiveParts > 100 {
		alarmKey := "clickhouse_small_parts_ratio"
		m.alarmCacheLock.RLock()
		prevAlarm, exists := m.alarmCache[alarmKey]
		m.alarmCacheLock.RUnlock()

		if !exists || prevAlarm.Status != "triggered" {
			event := &pb.AlarmEvent{
				Id:          uuid.New().String(),
				AlarmId:     alarmKey,
				AgentId:     m.agentID,
				Status:      "triggered",
				MetricName:  "clickhouse_small_parts_ratio",
				MetricValue: fmt.Sprintf("%.2f", metrics.SmallPartsRatio*100),
				Message:     fmt.Sprintf("High ratio of small parts: %.1f%% (%d small parts out of %d total). This may indicate frequent small inserts or merge issues.", metrics.SmallPartsRatio*100, metrics.SmallPartsCount, metrics.ActiveParts),
				Timestamp:   time.Now().Format(time.RFC3339),
				Severity:    "warning",
			}
			m.sendAndCacheAlarm(alarmKey, event)
		}
	}
}

// checkMemoryPressure checks memory pressure metrics
func (m *AlarmMonitor) checkMemoryPressure() {
	metrics, err := m.clickhouseCollector.CollectMemoryPressureMetrics()
	if err != nil || metrics == nil {
		return
	}

	thresholds := DefaultClickHouseThresholds()

	// Use thresholds from server if available
	memoryWarn := thresholds.MemoryUsageWarn
	memoryCrit := thresholds.MemoryUsageCritical
	if m.thresholds != nil && m.thresholds.MemoryThreshold > 0 {
		memoryCrit = m.thresholds.MemoryThreshold
		memoryWarn = memoryCrit * 0.85
	}

	// Check memory usage
	if metrics.MemoryUsagePercent > 0 {
		alarmKey := "clickhouse_memory_pressure"
		m.alarmCacheLock.RLock()
		prevAlarm, exists := m.alarmCache[alarmKey]
		m.alarmCacheLock.RUnlock()

		if metrics.MemoryUsagePercent >= memoryCrit {
			if !exists || prevAlarm.Status != "triggered" || prevAlarm.Severity != "critical" {
				event := &pb.AlarmEvent{
					Id:          uuid.New().String(),
					AlarmId:     alarmKey,
					AgentId:     m.agentID,
					Status:      "triggered",
					MetricName:  "clickhouse_memory_pressure",
					MetricValue: fmt.Sprintf("%.2f", metrics.MemoryUsagePercent),
					Message:     fmt.Sprintf("Critical memory usage: %.1f%% (threshold: %.1f%%). MemoryTracking: %d bytes. Queries may be killed.", metrics.MemoryUsagePercent, memoryCrit, metrics.MemoryTracking),
					Timestamp:   time.Now().Format(time.RFC3339),
					Severity:    "critical",
				}
				m.sendAndCacheAlarm(alarmKey, event)
			}
		} else if metrics.MemoryUsagePercent >= memoryWarn {
			if !exists || prevAlarm.Status != "triggered" {
				event := &pb.AlarmEvent{
					Id:          uuid.New().String(),
					AlarmId:     alarmKey,
					AgentId:     m.agentID,
					Status:      "triggered",
					MetricName:  "clickhouse_memory_pressure",
					MetricValue: fmt.Sprintf("%.2f", metrics.MemoryUsagePercent),
					Message:     fmt.Sprintf("High memory usage: %.1f%% (threshold: %.1f%%)", metrics.MemoryUsagePercent, memoryWarn),
					Timestamp:   time.Now().Format(time.RFC3339),
					Severity:    "warning",
				}
				m.sendAndCacheAlarm(alarmKey, event)
			}
		} else if exists && prevAlarm.Status == "triggered" {
			event := &pb.AlarmEvent{
				Id:          uuid.New().String(),
				AlarmId:     alarmKey,
				AgentId:     m.agentID,
				Status:      "resolved",
				MetricName:  "clickhouse_memory_pressure",
				MetricValue: fmt.Sprintf("%.2f", metrics.MemoryUsagePercent),
				Message:     fmt.Sprintf("Memory usage normalized: %.1f%%", metrics.MemoryUsagePercent),
				Timestamp:   time.Now().Format(time.RFC3339),
				Severity:    "info",
			}
			m.sendAndCacheAlarm(alarmKey, event)
		}
	}

	// Check QueryMemoryLimitExceeded
	if metrics.QueryMemoryLimitExceeded > 0 {
		alarmKey := "clickhouse_query_memory_exceeded"
		m.alarmCacheLock.RLock()
		prevAlarm, exists := m.alarmCache[alarmKey]
		m.alarmCacheLock.RUnlock()

		// Rate limit: only report once per 5 minutes
		shouldReport := !exists
		if exists {
			prevTime, err := time.Parse(time.RFC3339, prevAlarm.Timestamp)
			if err == nil && time.Since(prevTime) > 5*time.Minute {
				shouldReport = true
			}
		}

		if shouldReport {
			event := &pb.AlarmEvent{
				Id:          uuid.New().String(),
				AlarmId:     alarmKey,
				AgentId:     m.agentID,
				Status:      "triggered",
				MetricName:  "clickhouse_query_memory_exceeded",
				MetricValue: fmt.Sprintf("%d", metrics.QueryMemoryLimitExceeded),
				Message:     fmt.Sprintf("Queries exceeding memory limit: %d occurrences. Queries are being killed due to memory pressure.", metrics.QueryMemoryLimitExceeded),
				Timestamp:   time.Now().Format(time.RFC3339),
				Severity:    "warning",
			}
			m.sendAndCacheAlarm(alarmKey, event)
		}
	}
}

// checkQueryConcurrency checks query concurrency metrics
func (m *AlarmMonitor) checkQueryConcurrency() {
	metrics, err := m.clickhouseCollector.CollectQueryConcurrencyMetrics()
	if err != nil || metrics == nil {
		return
	}

	thresholds := DefaultClickHouseThresholds()

	// Check long running queries
	if metrics.VeryLongRunningQueries > 0 {
		alarmKey := "clickhouse_very_long_query"
		m.alarmCacheLock.RLock()
		prevAlarm, exists := m.alarmCache[alarmKey]
		m.alarmCacheLock.RUnlock()

		// Rate limit
		shouldReport := !exists
		if exists {
			prevTime, err := time.Parse(time.RFC3339, prevAlarm.Timestamp)
			if err == nil && time.Since(prevTime) > 5*time.Minute {
				shouldReport = true
			}
		}

		if shouldReport {
			var queryDetails strings.Builder
			for i, q := range metrics.LongRunningQueryDetails {
				if i >= 3 {
					break
				}
				queryDetails.WriteString(fmt.Sprintf("- %s (%.0fs, %s): %s...\n", q.User, q.ElapsedTime, q.Database, truncate(q.Query, 100)))
			}

			event := &pb.AlarmEvent{
				Id:          uuid.New().String(),
				AlarmId:     alarmKey,
				AgentId:     m.agentID,
				Status:      "triggered",
				MetricName:  "clickhouse_very_long_query",
				MetricValue: fmt.Sprintf("%.2f", metrics.MaxQueryElapsed),
				Message:     fmt.Sprintf("Very long running queries detected: %d queries > 5min. Max elapsed: %.0fs.\n%s", metrics.VeryLongRunningQueries, metrics.MaxQueryElapsed, queryDetails.String()),
				Timestamp:   time.Now().Format(time.RFC3339),
				Severity:    "critical",
			}
			m.sendAndCacheAlarm(alarmKey, event)
		}
	} else if metrics.LongRunningQueries > 0 && metrics.MaxQueryElapsed > thresholds.LongRunningQueryWarn {
		alarmKey := "clickhouse_long_query"
		m.alarmCacheLock.RLock()
		prevAlarm, exists := m.alarmCache[alarmKey]
		m.alarmCacheLock.RUnlock()

		shouldReport := !exists
		if exists {
			prevTime, err := time.Parse(time.RFC3339, prevAlarm.Timestamp)
			if err == nil && time.Since(prevTime) > 5*time.Minute {
				shouldReport = true
			}
		}

		if shouldReport {
			event := &pb.AlarmEvent{
				Id:          uuid.New().String(),
				AlarmId:     alarmKey,
				AgentId:     m.agentID,
				Status:      "triggered",
				MetricName:  "clickhouse_long_query",
				MetricValue: fmt.Sprintf("%.2f", metrics.MaxQueryElapsed),
				Message:     fmt.Sprintf("Long running queries: %d queries > 60s. Max elapsed: %.0fs", metrics.LongRunningQueries, metrics.MaxQueryElapsed),
				Timestamp:   time.Now().Format(time.RFC3339),
				Severity:    "warning",
			}
			m.sendAndCacheAlarm(alarmKey, event)
		}
	}

	// Check query concurrency
	if metrics.RunningQueries > thresholds.MaxRunningQueries {
		alarmKey := "clickhouse_high_concurrency"
		m.alarmCacheLock.RLock()
		prevAlarm, exists := m.alarmCache[alarmKey]
		m.alarmCacheLock.RUnlock()

		if !exists || prevAlarm.Status != "triggered" {
			event := &pb.AlarmEvent{
				Id:          uuid.New().String(),
				AlarmId:     alarmKey,
				AgentId:     m.agentID,
				Status:      "triggered",
				MetricName:  "clickhouse_high_concurrency",
				MetricValue: fmt.Sprintf("%d", metrics.RunningQueries),
				Message:     fmt.Sprintf("High query concurrency: %d running queries (threshold: %d). SELECT: %d, INSERT: %d", metrics.RunningQueries, thresholds.MaxRunningQueries, metrics.RunningSelectQueries, metrics.RunningInsertQueries),
				Timestamp:   time.Now().Format(time.RFC3339),
				Severity:    "warning",
			}
			m.sendAndCacheAlarm(alarmKey, event)
		}
	}
}

// checkReplicationStatus checks replication health
func (m *AlarmMonitor) checkReplicationStatus() {
	replicas, err := m.clickhouseCollector.CollectReplicaMetrics()
	if err != nil || len(replicas) == 0 {
		return
	}

	thresholds := DefaultClickHouseThresholds()

	// Check for readonly replicas
	for _, r := range replicas {
		if r.IsReadonly {
			alarmKey := fmt.Sprintf("clickhouse_replica_readonly_%s_%s", r.Database, r.Table)
			m.alarmCacheLock.RLock()
			prevAlarm, exists := m.alarmCache[alarmKey]
			m.alarmCacheLock.RUnlock()

			if !exists || prevAlarm.Status != "triggered" {
				event := &pb.AlarmEvent{
					Id:          uuid.New().String(),
					AlarmId:     alarmKey,
					AgentId:     m.agentID,
					Status:      "triggered",
					MetricName:  "clickhouse_replica_readonly",
					MetricValue: fmt.Sprintf("%s.%s", r.Database, r.Table),
					Message:     fmt.Sprintf("Replica is READONLY: %s.%s. This may indicate ZooKeeper issues or network problems.", r.Database, r.Table),
					Timestamp:   time.Now().Format(time.RFC3339),
					Severity:    "critical",
					Database:    r.Database,
				}
				m.sendAndCacheAlarm(alarmKey, event)
			}
		}

		// Check replication delay
		if r.AbsoluteDelay > thresholds.ReplicationDelayCrit {
			alarmKey := fmt.Sprintf("clickhouse_replication_delay_%s_%s", r.Database, r.Table)
			m.alarmCacheLock.RLock()
			prevAlarm, exists := m.alarmCache[alarmKey]
			m.alarmCacheLock.RUnlock()

			if !exists || prevAlarm.Status != "triggered" || prevAlarm.Severity != "critical" {
				event := &pb.AlarmEvent{
					Id:          uuid.New().String(),
					AlarmId:     alarmKey,
					AgentId:     m.agentID,
					Status:      "triggered",
					MetricName:  "clickhouse_replication_delay",
					MetricValue: fmt.Sprintf("%d", r.AbsoluteDelay),
					Message:     fmt.Sprintf("Critical replication delay: %s.%s is %d seconds behind (threshold: %d). Queue size: %d", r.Database, r.Table, r.AbsoluteDelay, thresholds.ReplicationDelayCrit, r.QueueSize),
					Timestamp:   time.Now().Format(time.RFC3339),
					Severity:    "critical",
					Database:    r.Database,
				}
				m.sendAndCacheAlarm(alarmKey, event)
			}
		} else if r.AbsoluteDelay > thresholds.ReplicationDelayWarn {
			alarmKey := fmt.Sprintf("clickhouse_replication_delay_%s_%s", r.Database, r.Table)
			m.alarmCacheLock.RLock()
			prevAlarm, exists := m.alarmCache[alarmKey]
			m.alarmCacheLock.RUnlock()

			if !exists || prevAlarm.Status != "triggered" {
				event := &pb.AlarmEvent{
					Id:          uuid.New().String(),
					AlarmId:     alarmKey,
					AgentId:     m.agentID,
					Status:      "triggered",
					MetricName:  "clickhouse_replication_delay",
					MetricValue: fmt.Sprintf("%d", r.AbsoluteDelay),
					Message:     fmt.Sprintf("Replication delay: %s.%s is %d seconds behind (threshold: %d)", r.Database, r.Table, r.AbsoluteDelay, thresholds.ReplicationDelayWarn),
					Timestamp:   time.Now().Format(time.RFC3339),
					Severity:    "warning",
					Database:    r.Database,
				}
				m.sendAndCacheAlarm(alarmKey, event)
			}
		}
	}
}

// checkMutations checks mutation status
func (m *AlarmMonitor) checkMutations() {
	mutations, err := m.clickhouseCollector.CollectMutationMetrics()
	if err != nil || len(mutations) == 0 {
		return
	}

	thresholds := DefaultClickHouseThresholds()

	for _, mut := range mutations {
		if mut.IsDone {
			continue
		}

		alarmKey := fmt.Sprintf("clickhouse_stuck_mutation_%s_%s_%s", mut.Database, mut.Table, mut.MutationID)
		m.alarmCacheLock.RLock()
		prevAlarm, exists := m.alarmCache[alarmKey]
		m.alarmCacheLock.RUnlock()

		if mut.AgeSeconds > thresholds.MutationAgeCritical {
			if !exists || prevAlarm.Status != "triggered" || prevAlarm.Severity != "critical" {
				event := &pb.AlarmEvent{
					Id:          uuid.New().String(),
					AlarmId:     alarmKey,
					AgentId:     m.agentID,
					Status:      "triggered",
					MetricName:  "clickhouse_stuck_mutation",
					MetricValue: fmt.Sprintf("%.0f", mut.AgeSeconds),
					Message:     fmt.Sprintf("Critical: Mutation stuck for %.1f hours: %s.%s (%s). Parts to do: %d. Reason: %s", mut.AgeSeconds/3600, mut.Database, mut.Table, mut.MutationID, mut.PartsToDo, mut.LatestFailReason),
					Timestamp:   time.Now().Format(time.RFC3339),
					Severity:    "critical",
					Database:    mut.Database,
				}
				m.sendAndCacheAlarm(alarmKey, event)
			}
		} else if mut.AgeSeconds > thresholds.MutationAgeWarn {
			if !exists || prevAlarm.Status != "triggered" {
				event := &pb.AlarmEvent{
					Id:          uuid.New().String(),
					AlarmId:     alarmKey,
					AgentId:     m.agentID,
					Status:      "triggered",
					MetricName:  "clickhouse_stuck_mutation",
					MetricValue: fmt.Sprintf("%.0f", mut.AgeSeconds),
					Message:     fmt.Sprintf("Long-running mutation: %s.%s (%s) running for %.1f hours. Parts to do: %d", mut.Database, mut.Table, mut.MutationID, mut.AgeSeconds/3600, mut.PartsToDo),
					Timestamp:   time.Now().Format(time.RFC3339),
					Severity:    "warning",
					Database:    mut.Database,
				}
				m.sendAndCacheAlarm(alarmKey, event)
			}
		}
	}
}

// checkClickHouseErrors checks for critical errors
func (m *AlarmMonitor) checkClickHouseErrors() {
	errors, err := m.clickhouseCollector.CollectClickHouseErrors()
	if err != nil || len(errors) == 0 {
		return
	}

	criticalErrors := []string{
		"TOO_MANY_PARTS",
		"MEMORY_LIMIT_EXCEEDED",
		"CANNOT_ALLOCATE_MEMORY",
		"TIMEOUT_EXCEEDED",
		"ZOOKEEPER_ERROR",
		"REPLICA_IS_NOT_IN_QUORUM",
		"NO_REPLICA_HAS_PART",
	}

	for _, e := range errors {
		isCritical := false
		for _, ce := range criticalErrors {
			if strings.Contains(e.Name, ce) {
				isCritical = true
				break
			}
		}

		if !isCritical {
			continue
		}

		alarmKey := fmt.Sprintf("clickhouse_error_%s", e.Name)
		m.alarmCacheLock.RLock()
		prevAlarm, exists := m.alarmCache[alarmKey]
		m.alarmCacheLock.RUnlock()

		// Rate limit per error type
		shouldReport := !exists
		if exists {
			prevTime, err := time.Parse(time.RFC3339, prevAlarm.Timestamp)
			if err == nil && time.Since(prevTime) > 5*time.Minute {
				shouldReport = true
			}
		}

		if shouldReport {
			severity := "warning"
			if strings.Contains(e.Name, "TOO_MANY_PARTS") || strings.Contains(e.Name, "CANNOT_ALLOCATE_MEMORY") {
				severity = "critical"
			}

			event := &pb.AlarmEvent{
				Id:          uuid.New().String(),
				AlarmId:     alarmKey,
				AgentId:     m.agentID,
				Status:      "triggered",
				MetricName:  "clickhouse_error",
				MetricValue: fmt.Sprintf("%d", e.Value),
				Message:     fmt.Sprintf("ClickHouse error: %s (count: %d). Last error: %s", e.Name, e.Value, truncate(e.LastErrorMessage, 200)),
				Timestamp:   time.Now().Format(time.RFC3339),
				Severity:    severity,
			}
			m.sendAndCacheAlarm(alarmKey, event)
		}
	}
}

// checkDiskUsage checks disk usage
func (m *AlarmMonitor) checkDiskUsage() {
	metrics, err := m.clickhouseCollector.CollectMetrics()
	if err != nil || metrics == nil {
		return
	}

	// Use thresholds from server if available
	diskWarn := 70.0
	diskCrit := 85.0
	if m.thresholds != nil && m.thresholds.DiskThreshold > 0 {
		diskCrit = m.thresholds.DiskThreshold
		diskWarn = diskCrit * 0.85
	}

	if metrics.DiskPercent > 0 {
		alarmKey := "clickhouse_disk_usage"
		m.alarmCacheLock.RLock()
		prevAlarm, exists := m.alarmCache[alarmKey]
		m.alarmCacheLock.RUnlock()

		if metrics.DiskPercent >= diskCrit {
			if !exists || prevAlarm.Status != "triggered" || prevAlarm.Severity != "critical" {
				event := &pb.AlarmEvent{
					Id:          uuid.New().String(),
					AlarmId:     alarmKey,
					AgentId:     m.agentID,
					Status:      "triggered",
					MetricName:  "clickhouse_disk_usage",
					MetricValue: fmt.Sprintf("%.2f", metrics.DiskPercent),
					Message:     fmt.Sprintf("Critical disk usage: %.1f%% (threshold: %.1f%%). Used: %d bytes, Available: %d bytes", metrics.DiskPercent, diskCrit, metrics.DiskUsage, metrics.DiskAvailable),
					Timestamp:   time.Now().Format(time.RFC3339),
					Severity:    "critical",
				}
				m.sendAndCacheAlarm(alarmKey, event)
			}
		} else if metrics.DiskPercent >= diskWarn {
			if !exists || prevAlarm.Status != "triggered" {
				event := &pb.AlarmEvent{
					Id:          uuid.New().String(),
					AlarmId:     alarmKey,
					AgentId:     m.agentID,
					Status:      "triggered",
					MetricName:  "clickhouse_disk_usage",
					MetricValue: fmt.Sprintf("%.2f", metrics.DiskPercent),
					Message:     fmt.Sprintf("High disk usage: %.1f%% (threshold: %.1f%%)", metrics.DiskPercent, diskWarn),
					Timestamp:   time.Now().Format(time.RFC3339),
					Severity:    "warning",
				}
				m.sendAndCacheAlarm(alarmKey, event)
			}
		} else if exists && prevAlarm.Status == "triggered" {
			event := &pb.AlarmEvent{
				Id:          uuid.New().String(),
				AlarmId:     alarmKey,
				AgentId:     m.agentID,
				Status:      "resolved",
				MetricName:  "clickhouse_disk_usage",
				MetricValue: fmt.Sprintf("%.2f", metrics.DiskPercent),
				Message:     fmt.Sprintf("Disk usage normalized: %.1f%%", metrics.DiskPercent),
				Timestamp:   time.Now().Format(time.RFC3339),
				Severity:    "info",
			}
			m.sendAndCacheAlarm(alarmKey, event)
		}
	}
}

// Helper functions

func (m *AlarmMonitor) cacheAlarm(key string, event *pb.AlarmEvent) {
	m.alarmCacheLock.Lock()
	m.alarmCache[key] = event
	m.alarmCacheLock.Unlock()
}

func (m *AlarmMonitor) sendAndCacheAlarm(key string, event *pb.AlarmEvent) {
	if err := m.reportAlarm(event); err != nil {
		logger.Error("Failed to send alarm %s: %v", key, err)
	} else {
		m.cacheAlarm(key, event)
		logger.Warning("Alarm %s %s (ID: %s)", key, event.Status, event.Id)
	}
}

// reportAlarm sends alarm event to API
func (m *AlarmMonitor) reportAlarm(event *pb.AlarmEvent) error {
	maxRetries := 3
	backoff := time.Second * 2

	for attempt := 0; attempt < maxRetries; attempt++ {
		if m.client == nil {
			if m.clientRefreshCallback != nil {
				if newClient, err := m.clientRefreshCallback(); err == nil {
					m.client = newClient
				}
			}
			if m.client == nil {
				return fmt.Errorf("gRPC client is nil")
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

		req := &pb.ReportAlarmRequest{
			AgentId: m.agentID,
			Events:  []*pb.AlarmEvent{event},
		}

		_, err := m.client.ReportAlarm(ctx, req)
		cancel()

		if err == nil {
			return nil
		}

		isConnectionError := strings.Contains(err.Error(), "connection") ||
			strings.Contains(err.Error(), "transport") ||
			strings.Contains(err.Error(), "Unavailable")

		if attempt < maxRetries-1 && isConnectionError {
			logger.Warning("Alarm report failed (attempt %d/%d): %v", attempt+1, maxRetries, err)
			if m.clientRefreshCallback != nil {
				if newClient, err := m.clientRefreshCallback(); err == nil {
					m.client = newClient
				}
			}
			time.Sleep(backoff * time.Duration(attempt+1))
			continue
		}

		return err
	}

	return fmt.Errorf("failed after %d retries", maxRetries)
}

// reportAgentVersion reports agent version to server
func (m *AlarmMonitor) reportAgentVersion() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	// Report immediately on start
	m.doReportVersion()

	for {
		select {
		case <-ticker.C:
			m.doReportVersion()
		case <-m.stopCh:
			return
		}
	}
}

func (m *AlarmMonitor) doReportVersion() {
	if m.client == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	versionInfo := &pb.AgentVersionInfo{
		Version:  AgentVersion,
		Platform: "clickhouse",
	}

	req := &pb.ReportVersionRequest{
		AgentId:     m.agentID,
		VersionInfo: versionInfo,
	}

	_, err := m.client.ReportVersion(ctx, req)
	if err != nil {
		logger.Debug("Failed to report version: %v", err)
	}
}

// truncate truncates a string to max length
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

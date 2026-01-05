package clickhouse

import (
	"context"
	"time"

	"github.com/CloudNativeWorks/clustereye-agent-clickhouse/internal/logger"
	"github.com/CloudNativeWorks/clustereye-agent-clickhouse/internal/model"
)

// CollectMetrics collects performance and resource metrics from ClickHouse
func (c *ClickhouseCollector) CollectMetrics() (*model.ClickhouseMetrics, error) {
	conn, err := c.GetConnection()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	metrics := &model.ClickhouseMetrics{
		CollectionTime: time.Now(),
	}

	// Get connection metrics
	var currentConns int64
	err = conn.QueryRow(ctx, `
		SELECT value FROM system.metrics WHERE metric = 'TCPConnection'
	`).Scan(&currentConns)
	if err != nil {
		logger.Debug("Failed to get connection count: %v", err)
	}
	metrics.CurrentConnections = currentConns

	// Get max connections from settings
	var maxConns int64
	err = conn.QueryRow(ctx, `
		SELECT value FROM system.settings WHERE name = 'max_concurrent_queries'
	`).Scan(&maxConns)
	if err != nil {
		logger.Debug("Failed to get max connections: %v", err)
		maxConns = 100 // default
	}
	metrics.MaxConnections = maxConns

	// Get query metrics
	var queriesPerSec, selectPerSec, insertPerSec uint64
	err = conn.QueryRow(ctx, `
		SELECT
			sum(ProfileEvent_Query),
			sum(ProfileEvent_SelectQuery),
			sum(ProfileEvent_InsertQuery)
		FROM system.metric_log
		WHERE event_time >= now() - INTERVAL 60 SECOND
	`).Scan(&queriesPerSec, &selectPerSec, &insertPerSec)

	if err != nil {
		logger.Debug("Failed to get query metrics from metric_log: %v", err)
		// Fallback to system.events
		var queryCount uint64
		err = conn.QueryRow(ctx, `
			SELECT value FROM system.events WHERE event = 'Query'
		`).Scan(&queryCount)
		if err == nil {
			queriesPerSec = queryCount
		}
	}

	metrics.QueriesPerSecond = float64(queriesPerSec) / 60.0
	metrics.SelectQueriesPerSecond = float64(selectPerSec) / 60.0
	metrics.InsertQueriesPerSecond = float64(insertPerSec) / 60.0

	// Get running queries
	var runningQueries uint64
	err = conn.QueryRow(ctx, `
		SELECT count() FROM system.processes
	`).Scan(&runningQueries)
	if err != nil {
		logger.Debug("Failed to get running queries: %v", err)
	}
	metrics.RunningQueries = int64(runningQueries)

	// Get memory metrics from OS-level metrics (more accurate)
	var osMemoryTotal, osMemoryAvailable float64
	err = conn.QueryRow(ctx, `
		SELECT value FROM system.asynchronous_metrics WHERE metric = 'OSMemoryTotal'
	`).Scan(&osMemoryTotal)
	if err != nil {
		logger.Debug("Failed to get OS total memory: %v", err)
	}

	err = conn.QueryRow(ctx, `
		SELECT value FROM system.asynchronous_metrics WHERE metric = 'OSMemoryAvailable'
	`).Scan(&osMemoryAvailable)
	if err != nil {
		logger.Debug("Failed to get OS available memory: %v", err)
	}

	// Calculate memory usage: Total - Available = Used
	if osMemoryTotal > 0 {
		memoryUsed := osMemoryTotal - osMemoryAvailable
		metrics.MemoryUsage = int64(memoryUsed)
		metrics.MemoryAvailable = int64(osMemoryAvailable)
		metrics.MemoryPercent = (memoryUsed / osMemoryTotal) * 100
	} else {
		// Fallback to ClickHouse's internal memory tracking
		var memoryTracking int64
		err = conn.QueryRow(ctx, `
			SELECT value FROM system.metrics WHERE metric = 'MemoryTracking'
		`).Scan(&memoryTracking)
		if err != nil {
			logger.Debug("Failed to get memory tracking: %v", err)
		}
		metrics.MemoryUsage = memoryTracking
	}

	// Get disk metrics from system.parts
	var diskUsage uint64
	err = conn.QueryRow(ctx, `
		SELECT sum(bytes_on_disk)
		FROM system.parts
		WHERE active = 1
	`).Scan(&diskUsage)
	if err != nil {
		logger.Debug("Failed to get disk usage: %v", err)
	} else {
		metrics.DiskUsage = int64(diskUsage)

		// Try to get free space from system.disks
		var freeSpace uint64
		err = conn.QueryRow(ctx, `SELECT sum(free_space) FROM system.disks`).Scan(&freeSpace)
		if err == nil {
			metrics.DiskAvailable = int64(freeSpace)
			totalDisk := int64(diskUsage + freeSpace)
			if totalDisk > 0 {
				metrics.DiskPercent = (float64(diskUsage) / float64(totalDisk)) * 100
			}
		}
	}

	// Get merge metrics
	var mergesInProgress int64
	err = conn.QueryRow(ctx, `
		SELECT value FROM system.metrics WHERE metric = 'Merge'
	`).Scan(&mergesInProgress)
	if err != nil {
		logger.Debug("Failed to get merge count: %v", err)
	}
	metrics.MergesInProgress = mergesInProgress

	var partsCount uint64
	err = conn.QueryRow(ctx, `
		SELECT count() FROM system.parts WHERE active = 1
	`).Scan(&partsCount)
	if err != nil {
		logger.Debug("Failed to get parts count: %v", err)
	}
	metrics.PartsCount = int64(partsCount)

	// Get read metrics
	var rowsRead, bytesRead uint64
	err = conn.QueryRow(ctx, `
		SELECT
			sum(ProfileEvent_SelectedRows),
			sum(ProfileEvent_SelectedBytes)
		FROM system.metric_log
		WHERE event_time >= now() - INTERVAL 60 SECOND
	`).Scan(&rowsRead, &bytesRead)
	if err != nil {
		logger.Debug("Failed to get read metrics: %v", err)
	}
	metrics.RowsRead = int64(rowsRead)
	metrics.BytesRead = int64(bytesRead)

	// Get network metrics
	var networkReceive, networkSend uint64
	err = conn.QueryRow(ctx, `
		SELECT
			sum(ProfileEvent_NetworkReceiveBytes),
			sum(ProfileEvent_NetworkSendBytes)
		FROM system.metric_log
		WHERE event_time >= now() - INTERVAL 60 SECOND
	`).Scan(&networkReceive, &networkSend)
	if err != nil {
		logger.Debug("Failed to get network metrics: %v", err)
	}
	metrics.NetworkReceiveBytes = int64(networkReceive)
	metrics.NetworkSendBytes = int64(networkSend)

	// Get CPU usage by calculating from idle time metrics
	// CPU Usage % = (1 - average_idle_time) * 100
	var avgIdleTime float64
	var cpuCount uint64
	err = conn.QueryRow(ctx, `
		SELECT
			avg(value) as avg_idle,
			count() as cpu_count
		FROM system.asynchronous_metrics
		WHERE metric LIKE 'OSIdleTimeCPU%'
	`).Scan(&avgIdleTime, &cpuCount)
	if err != nil {
		logger.Debug("Failed to get CPU idle time: %v", err)
		metrics.CPUUsage = 0
	} else {
		// Idle time is between 0 and 1, so CPU usage = (1 - idle) * 100
		metrics.CPUUsage = (1.0 - avgIdleTime) * 100
		if metrics.CPUUsage < 0 {
			metrics.CPUUsage = 0
		}
		if metrics.CPUUsage > 100 {
			metrics.CPUUsage = 100
		}
	}

	// Get cache metrics
	var markCacheBytes, markCacheFiles int64
	err = conn.QueryRow(ctx, `
		SELECT value FROM system.metrics WHERE metric = 'MarkCacheBytes'
	`).Scan(&markCacheBytes)
	if err != nil {
		logger.Debug("Failed to get mark cache bytes: %v", err)
	}
	metrics.MarkCacheBytes = markCacheBytes

	err = conn.QueryRow(ctx, `
		SELECT value FROM system.metrics WHERE metric = 'MarkCacheFiles'
	`).Scan(&markCacheFiles)
	if err != nil {
		logger.Debug("Failed to get mark cache files: %v", err)
	}
	metrics.MarkCacheFiles = markCacheFiles

	// Measure response time (SELECT 1 query latency)
	responseTime := c.measureResponseTime()
	metrics.ResponseTimeMs = responseTime
	logger.Debug("ClickHouse response time: %.3f ms", responseTime)

	return metrics, nil
}

// measureResponseTime measures the response time of a simple SELECT 1 query
func (c *ClickhouseCollector) measureResponseTime() float64 {
	start := time.Now()

	conn, err := c.GetConnection()
	if err != nil {
		logger.Debug("Failed to get connection for response time measurement: %v", err)
		return -1.0
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var result uint8 // ClickHouse returns UInt8 for SELECT 1
	err = conn.QueryRow(ctx, "SELECT 1").Scan(&result)
	if err != nil {
		logger.Debug("Failed to execute response time query: %v", err)
		return -1.0
	}

	duration := time.Since(start)
	responseTimeMs := float64(duration.Nanoseconds()) / float64(time.Millisecond)

	return responseTimeMs
}

// CollectQueries collects query execution information
func (c *ClickhouseCollector) CollectQueries() ([]model.ClickhouseQuery, error) {
	conn, err := c.GetConnection()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	queries := make([]model.ClickhouseQuery, 0)

	// Get recent queries from query_log
	rows, err := conn.Query(ctx, `
		SELECT
			query_id,
			query,
			user,
			query_start_time,
			query_duration_ms,
			memory_usage,
			read_rows,
			read_bytes,
			written_rows,
			written_bytes,
			result_rows,
			result_bytes,
			type,
			databases,
			tables,
			exception
		FROM system.query_log
		WHERE event_time >= now() - INTERVAL ? SECOND
			AND type IN ('QueryFinish', 'ExceptionWhileProcessing')
		ORDER BY query_start_time DESC
		LIMIT ?
	`, c.cfg.Clickhouse.QueryMonitoring.CollectionIntervalSec,
		c.cfg.Clickhouse.QueryMonitoring.MaxQueriesPerCollection)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var q model.ClickhouseQuery
		var databases, tables []string
		var queryType string
		var queryDurationMs, memoryUsage, readRows, readBytes, writtenRows, writtenBytes, resultRows, resultBytes uint64

		err := rows.Scan(
			&q.QueryID,
			&q.Query,
			&q.User,
			&q.QueryStartTime,
			&queryDurationMs,
			&memoryUsage,
			&readRows,
			&readBytes,
			&writtenRows,
			&writtenBytes,
			&resultRows,
			&resultBytes,
			&queryType,
			&databases,
			&tables,
			&q.Exception,
		)

		if err != nil {
			logger.Warning("Failed to scan query row: %v", err)
			continue
		}

		// Convert UInt64 to appropriate types
		q.QueryDuration = float64(queryDurationMs)
		q.MemoryUsage = int64(memoryUsage)
		q.ReadRows = int64(readRows)
		q.ReadBytes = int64(readBytes)
		q.WrittenRows = int64(writtenRows)
		q.WrittenBytes = int64(writtenBytes)
		q.ResultRows = int64(resultRows)
		q.ResultBytes = int64(resultBytes)

		// Map query type (from Enum8 string value)
		switch queryType {
		case "QueryStart":
			q.QueryType = "RUNNING"
		case "QueryFinish":
			q.QueryType = "SELECT"
		case "ExceptionBeforeStart", "ExceptionWhileProcessing":
			q.QueryType = "ERROR"
		default:
			q.QueryType = queryType
		}

		// Get first database
		if len(databases) > 0 {
			q.Database = databases[0]
		}
		q.Tables = tables

		// Truncate query text if needed
		maxLen := c.cfg.Clickhouse.QueryMonitoring.MaxQueryTextLength
		if maxLen > 0 && len(q.Query) > maxLen {
			q.Query = q.Query[:maxLen] + "..."
		}

		// Check if slow query
		if q.QueryDuration >= float64(c.cfg.Clickhouse.QueryMonitoring.SlowQueryThresholdMs) {
			q.IsSlowQuery = true
		}

		queries = append(queries, q)
	}

	return queries, nil
}

// CollectTableMetrics collects metrics for individual tables
func (c *ClickhouseCollector) CollectTableMetrics() ([]model.TableMetric, error) {
	conn, err := c.GetConnection()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tables := make([]model.TableMetric, 0)

	rows, err := conn.Query(ctx, `
		SELECT
			database,
			table,
			engine,
			sum(rows) as total_rows,
			sum(bytes) as total_bytes,
			count() as parts_count,
			sum(active) as active_parts,
			sum(bytes_on_disk) as compressed_size,
			sum(data_uncompressed_bytes) as uncompressed_size,
			max(modification_time) as last_modified
		FROM system.parts
		WHERE database NOT IN ('system', 'information_schema', 'INFORMATION_SCHEMA')
		GROUP BY database, table, engine
		ORDER BY total_bytes DESC
		LIMIT 100
	`)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var t model.TableMetric
		var totalRows, totalBytes, partsCount, activeParts, compressedSize, uncompressedSize uint64

		err := rows.Scan(
			&t.Database,
			&t.Table,
			&t.Engine,
			&totalRows,
			&totalBytes,
			&partsCount,
			&activeParts,
			&compressedSize,
			&uncompressedSize,
			&t.LastModifiedTime,
		)

		if err != nil {
			logger.Warning("Failed to scan table metric row: %v", err)
			continue
		}

		// Convert UInt64 to int64
		t.TotalRows = int64(totalRows)
		t.TotalBytes = int64(totalBytes)
		t.PartsCount = int64(partsCount)
		t.ActiveParts = int64(activeParts)
		t.CompressedSize = int64(compressedSize)
		t.UncompressedSize = int64(uncompressedSize)

		// Calculate compression ratio
		if t.UncompressedSize > 0 {
			t.CompressionRatio = float64(t.CompressedSize) / float64(t.UncompressedSize)
		}

		// Check if replicated
		if len(t.Engine) > 10 && t.Engine[:10] == "Replicated" {
			t.IsReplicated = true
		}

		tables = append(tables, t)
	}

	return tables, nil
}

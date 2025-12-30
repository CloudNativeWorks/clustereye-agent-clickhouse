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
	var queriesPerSec, selectPerSec, insertPerSec float64
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
		err = conn.QueryRow(ctx, `
			SELECT value FROM system.events WHERE event = 'Query'
		`).Scan(&queriesPerSec)
		if err == nil {
			queriesPerSec = queriesPerSec / 60.0 // approximate per second
		}
	} else {
		queriesPerSec = queriesPerSec / 60.0
		selectPerSec = selectPerSec / 60.0
		insertPerSec = insertPerSec / 60.0
	}

	metrics.QueriesPerSecond = queriesPerSec
	metrics.SelectQueriesPerSecond = selectPerSec
	metrics.InsertQueriesPerSecond = insertPerSec

	// Get running queries
	var runningQueries int64
	err = conn.QueryRow(ctx, `
		SELECT count() FROM system.processes
	`).Scan(&runningQueries)
	if err != nil {
		logger.Debug("Failed to get running queries: %v", err)
	}
	metrics.RunningQueries = runningQueries

	// Get memory metrics
	var memoryUsage, memoryAvailable int64
	err = conn.QueryRow(ctx, `
		SELECT value FROM system.metrics WHERE metric = 'MemoryTracking'
	`).Scan(&memoryUsage)
	if err != nil {
		logger.Debug("Failed to get memory usage: %v", err)
	}
	metrics.MemoryUsage = memoryUsage

	// Get total memory from system
	err = conn.QueryRow(ctx, `
		SELECT value FROM system.asynchronous_metrics WHERE metric = 'MemoryAvailable'
	`).Scan(&memoryAvailable)
	if err != nil {
		logger.Debug("Failed to get available memory: %v", err)
	}
	metrics.MemoryAvailable = memoryAvailable

	if memoryAvailable > 0 {
		totalMemory := memoryUsage + memoryAvailable
		metrics.MemoryPercent = (float64(memoryUsage) / float64(totalMemory)) * 100
	}

	// Get disk metrics
	var diskUsage, diskFree int64
	err = conn.QueryRow(ctx, `
		SELECT
			sum(bytes_on_disk) as used,
			sum(free_space) as free
		FROM system.disks
	`).Scan(&diskUsage, &diskFree)
	if err != nil {
		logger.Debug("Failed to get disk metrics: %v", err)
	} else {
		metrics.DiskUsage = diskUsage
		metrics.DiskAvailable = diskFree
		totalDisk := diskUsage + diskFree
		if totalDisk > 0 {
			metrics.DiskPercent = (float64(diskUsage) / float64(totalDisk)) * 100
		}
	}

	// Get merge metrics
	var mergesInProgress, partsCount int64
	err = conn.QueryRow(ctx, `
		SELECT value FROM system.metrics WHERE metric = 'Merge'
	`).Scan(&mergesInProgress)
	if err != nil {
		logger.Debug("Failed to get merge count: %v", err)
	}
	metrics.MergesInProgress = mergesInProgress

	err = conn.QueryRow(ctx, `
		SELECT sum(parts) FROM system.parts WHERE active = 1
	`).Scan(&partsCount)
	if err != nil {
		logger.Debug("Failed to get parts count: %v", err)
	}
	metrics.PartsCount = partsCount

	// Get read metrics
	var rowsRead, bytesRead int64
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
	metrics.RowsRead = rowsRead
	metrics.BytesRead = bytesRead

	// Get network metrics
	var networkReceive, networkSend int64
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
	metrics.NetworkReceiveBytes = networkReceive
	metrics.NetworkSendBytes = networkSend

	// Get CPU usage
	var cpuUsage float64
	err = conn.QueryRow(ctx, `
		SELECT value FROM system.asynchronous_metrics WHERE metric = 'OSCPUVirtualTimeMicroseconds'
	`).Scan(&cpuUsage)
	if err != nil {
		logger.Debug("Failed to get CPU usage: %v", err)
	}
	metrics.CPUUsage = cpuUsage

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

	return metrics, nil
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
		var queryType int8

		err := rows.Scan(
			&q.QueryID,
			&q.Query,
			&q.User,
			&q.QueryStartTime,
			&q.QueryDuration,
			&q.MemoryUsage,
			&q.ReadRows,
			&q.ReadBytes,
			&q.WrittenRows,
			&q.WrittenBytes,
			&q.ResultRows,
			&q.ResultBytes,
			&queryType,
			&databases,
			&tables,
			&q.Exception,
		)

		if err != nil {
			logger.Warning("Failed to scan query row: %v", err)
			continue
		}

		// Map query type
		switch queryType {
		case 1:
			q.QueryType = "SELECT"
		case 2:
			q.QueryType = "INSERT"
		case 3:
			q.QueryType = "DDL"
		default:
			q.QueryType = "OTHER"
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

		err := rows.Scan(
			&t.Database,
			&t.Table,
			&t.Engine,
			&t.TotalRows,
			&t.TotalBytes,
			&t.PartsCount,
			&t.ActiveParts,
			&t.CompressedSize,
			&t.UncompressedSize,
			&t.LastModifiedTime,
		)

		if err != nil {
			logger.Warning("Failed to scan table metric row: %v", err)
			continue
		}

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

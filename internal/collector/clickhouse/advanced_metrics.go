package clickhouse

import (
	"context"
	"time"

	"github.com/CloudNativeWorks/clustereye-agent-clickhouse/internal/logger"
	"github.com/CloudNativeWorks/clustereye-agent-clickhouse/internal/model"
)

// CollectMergeMetrics collects metrics about merge operations from system.merges
// This is CRITICAL for ClickHouse performance - merge backlog = future problems
func (c *ClickhouseCollector) CollectMergeMetrics() (*model.MergeMetrics, error) {
	conn, err := c.GetConnection()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	metrics := &model.MergeMetrics{
		CollectionTime: time.Now(),
	}

	// Get aggregate merge metrics from system.merges
	var activeMerges, mutationMerges uint64
	var totalElapsed, maxElapsed float64
	var bytesRead, bytesWritten, rowsRead, rowsWritten uint64

	err = conn.QueryRow(ctx, `
		SELECT
			count() as active_merges,
			countIf(is_mutation = 1) as mutation_merges,
			sum(elapsed) as total_elapsed,
			max(elapsed) as max_elapsed,
			sum(bytes_read_uncompressed) as bytes_read,
			sum(bytes_written_uncompressed) as bytes_written,
			sum(rows_read) as rows_read,
			sum(rows_written) as rows_written
		FROM system.merges
	`).Scan(&activeMerges, &mutationMerges, &totalElapsed, &maxElapsed, &bytesRead, &bytesWritten, &rowsRead, &rowsWritten)

	if err != nil {
		logger.Debug("Failed to get merge metrics: %v", err)
		// Return empty metrics instead of error
		return metrics, nil
	}

	metrics.ActiveMerges = int64(activeMerges)
	metrics.MutationMerges = int64(mutationMerges)
	metrics.BackgroundMerges = int64(activeMerges) - int64(mutationMerges)
	metrics.TotalMergeTime = totalElapsed
	metrics.MaxMergeElapsed = maxElapsed
	metrics.MergeBytesRead = int64(bytesRead)
	metrics.MergeBytesWritten = int64(bytesWritten)
	metrics.MergeRowsRead = int64(rowsRead)
	metrics.MergeRowsWritten = int64(rowsWritten)

	// Calculate average
	if activeMerges > 0 {
		metrics.AvgMergeElapsed = totalElapsed / float64(activeMerges)
		// Calculate throughput (MB/s)
		if totalElapsed > 0 {
			metrics.MergeThroughputMBs = float64(bytesWritten) / (1024 * 1024) / totalElapsed
		}
	}

	// Get merge backlog from system.replication_queue (if replicated)
	var mergeBacklog uint64
	err = conn.QueryRow(ctx, `
		SELECT count() FROM system.replication_queue WHERE type = 'MERGE_PARTS'
	`).Scan(&mergeBacklog)
	if err == nil {
		metrics.MergeBacklog = int64(mergeBacklog)
	}

	// Get merge queue size from metrics
	var mergeQueueSize int64
	err = conn.QueryRow(ctx, `
		SELECT value FROM system.metrics WHERE metric = 'BackgroundMergesInProgress'
	`).Scan(&mergeQueueSize)
	if err == nil {
		metrics.MergeQueueSize = mergeQueueSize
	}

	// Get per-table merge info (only tables with active merges)
	rows, err := conn.Query(ctx, `
		SELECT
			database,
			table,
			count() as active_merges,
			avg(progress) as merge_progress,
			sum(bytes_written_uncompressed) as bytes_processed,
			max(elapsed) as elapsed_time,
			max(is_mutation) as is_mutation
		FROM system.merges
		GROUP BY database, table
		ORDER BY active_merges DESC
		LIMIT 20
	`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var info model.TableMergeInfo
			var activeMerges uint64
			var isMutation uint8

			err := rows.Scan(
				&info.Database,
				&info.Table,
				&activeMerges,
				&info.MergeProgress,
				&info.BytesProcessed,
				&info.ElapsedTime,
				&isMutation,
			)
			if err != nil {
				continue
			}
			info.ActiveMerges = int64(activeMerges)
			info.IsMutation = isMutation == 1
			metrics.TableMerges = append(metrics.TableMerges, info)
		}
	}

	return metrics, nil
}

// CollectPartsMetrics collects metrics about parts (ClickHouse's equivalent of PostgreSQL bloat)
func (c *ClickhouseCollector) CollectPartsMetrics() (*model.PartsMetrics, error) {
	conn, err := c.GetConnection()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	metrics := &model.PartsMetrics{
		CollectionTime: time.Now(),
	}

	// Get global part counts
	var totalParts, activeParts, smallParts uint64
	var totalBytes, totalRows uint64

	err = conn.QueryRow(ctx, `
		SELECT
			count() as total_parts,
			countIf(active = 1) as active_parts,
			countIf(active = 1 AND bytes_on_disk < 10485760) as small_parts,
			sum(bytes_on_disk) as total_bytes,
			sum(rows) as total_rows
		FROM system.parts
	`).Scan(&totalParts, &activeParts, &smallParts, &totalBytes, &totalRows)

	if err != nil {
		logger.Debug("Failed to get parts metrics: %v", err)
		return metrics, nil
	}

	metrics.TotalParts = int64(totalParts)
	metrics.ActiveParts = int64(activeParts)
	metrics.InactiveParts = int64(totalParts) - int64(activeParts)
	metrics.SmallPartsCount = int64(smallParts)
	metrics.TotalBytesOnDisk = int64(totalBytes)
	metrics.TotalRows = int64(totalRows)

	// Calculate small parts ratio
	if activeParts > 0 {
		metrics.SmallPartsRatio = float64(smallParts) / float64(activeParts)
	}

	// Get max parts per table
	var maxPartsPerTable uint64
	err = conn.QueryRow(ctx, `
		SELECT max(cnt) FROM (
			SELECT count() as cnt
			FROM system.parts
			WHERE active = 1
			GROUP BY database, table
		)
	`).Scan(&maxPartsPerTable)
	if err == nil {
		metrics.MaxPartsPerTable = int64(maxPartsPerTable)
	}

	// Get tables with too many parts (> 300 parts is concerning)
	rows, err := conn.Query(ctx, `
		SELECT
			database,
			table,
			count() as parts_count,
			countIf(active = 1) as active_parts,
			countIf(active = 1 AND bytes_on_disk < 10485760) as small_parts,
			sum(bytes_on_disk) as total_bytes,
			sum(rows) as total_rows,
			if(count() > 0, sum(bytes_on_disk) / count(), 0) as avg_part_size,
			any(engine) as engine
		FROM system.parts
		WHERE active = 1
		GROUP BY database, table
		HAVING active_parts > 300
		ORDER BY active_parts DESC
		LIMIT 50
	`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var info model.TablePartsInfo
			var partsCount, activeParts, smallParts, totalBytes, totalRows uint64
			var avgPartSize float64

			err := rows.Scan(
				&info.Database,
				&info.Table,
				&partsCount,
				&activeParts,
				&smallParts,
				&totalBytes,
				&totalRows,
				&avgPartSize,
				&info.Engine,
			)
			if err != nil {
				continue
			}
			info.PartsCount = int64(partsCount)
			info.ActiveParts = int64(activeParts)
			info.SmallParts = int64(smallParts)
			info.TotalBytes = int64(totalBytes)
			info.TotalRows = int64(totalRows)
			info.AvgPartSize = int64(avgPartSize)
			metrics.TablesWithTooManyParts = append(metrics.TablesWithTooManyParts, info)
		}
	}

	return metrics, nil
}

// CollectMemoryPressureMetrics collects memory-related metrics
func (c *ClickhouseCollector) CollectMemoryPressureMetrics() (*model.MemoryPressureMetrics, error) {
	conn, err := c.GetConnection()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	metrics := &model.MemoryPressureMetrics{
		CollectionTime: time.Now(),
	}

	// Get memory tracking metrics from system.metrics
	memoryMetrics := map[string]*int64{
		"MemoryTracking":             &metrics.MemoryTracking,
		"MemoryTrackingForMerges":    &metrics.MemoryTrackingForMerges,
		"MemoryTrackingInBackground": &metrics.MemoryTrackingForQueries, // Alternative name
	}

	for metricName, target := range memoryMetrics {
		var value int64
		err := conn.QueryRow(ctx, `
			SELECT value FROM system.metrics WHERE metric = ?
		`, metricName).Scan(&value)
		if err == nil {
			*target = value
		}
	}

	// Get max memory usage setting
	var maxMemory uint64
	err = conn.QueryRow(ctx, `
		SELECT value FROM system.settings WHERE name = 'max_server_memory_usage'
	`).Scan(&maxMemory)
	if err == nil {
		metrics.MaxServerMemoryUsage = int64(maxMemory)
		if maxMemory > 0 {
			metrics.MemoryUsagePercent = float64(metrics.MemoryTracking) / float64(maxMemory) * 100
		}
	}

	// Get memory pressure events from system.events
	eventMetrics := map[string]*int64{
		"QueryMemoryLimitExceeded": &metrics.QueryMemoryLimitExceeded,
		"MemoryOvercommitWaitTime": &metrics.MemoryOvercommitWaitTime,
	}

	for eventName, target := range eventMetrics {
		var value uint64
		err := conn.QueryRow(ctx, `
			SELECT value FROM system.events WHERE event = ?
		`, eventName).Scan(&value)
		if err == nil {
			*target = int64(value)
		}
	}

	// Get cache bytes
	var markCacheBytes, uncompressedCacheBytes int64
	err = conn.QueryRow(ctx, `SELECT value FROM system.metrics WHERE metric = 'MarkCacheBytes'`).Scan(&markCacheBytes)
	if err == nil {
		metrics.MarkCacheBytes = markCacheBytes
	}

	err = conn.QueryRow(ctx, `SELECT value FROM system.metrics WHERE metric = 'UncompressedCacheBytes'`).Scan(&uncompressedCacheBytes)
	if err == nil {
		metrics.UncompressedCacheBytes = uncompressedCacheBytes
	}

	return metrics, nil
}

// CollectQueryConcurrencyMetrics collects query execution metrics
func (c *ClickhouseCollector) CollectQueryConcurrencyMetrics() (*model.QueryConcurrencyMetrics, error) {
	conn, err := c.GetConnection()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	metrics := &model.QueryConcurrencyMetrics{
		CollectionTime: time.Now(),
	}

	// Get running query counts from system.processes
	var runningQueries, runningSelect, runningInsert uint64
	var longRunning, veryLongRunning uint64
	var maxElapsed float64
	var totalMemory int64 // ClickHouse returns Int64 for sum(memory_usage)
	var totalReadRows, totalReadBytes uint64

	err = conn.QueryRow(ctx, `
		SELECT
			count() as running_queries,
			countIf(query_kind = 'Select') as running_select,
			countIf(query_kind = 'Insert') as running_insert,
			countIf(elapsed > 60) as long_running,
			countIf(elapsed > 300) as very_long_running,
			max(elapsed) as max_elapsed,
			sum(memory_usage) as total_memory,
			sum(read_rows) as total_read_rows,
			sum(read_bytes) as total_read_bytes
		FROM system.processes
		WHERE is_cancelled = 0
	`).Scan(&runningQueries, &runningSelect, &runningInsert, &longRunning, &veryLongRunning,
		&maxElapsed, &totalMemory, &totalReadRows, &totalReadBytes)

	if err != nil {
		logger.Debug("Failed to get query concurrency metrics: %v", err)
		return metrics, nil
	}

	metrics.RunningQueries = int64(runningQueries)
	metrics.RunningSelectQueries = int64(runningSelect)
	metrics.RunningInsertQueries = int64(runningInsert)
	metrics.LongRunningQueries = int64(longRunning)
	metrics.VeryLongRunningQueries = int64(veryLongRunning)
	metrics.MaxQueryElapsed = maxElapsed
	metrics.TotalQueryMemoryUsage = totalMemory // Already int64
	metrics.TotalReadRows = int64(totalReadRows)
	metrics.TotalReadBytes = int64(totalReadBytes)

	// Get queued queries from metrics
	var queuedQueries int64
	err = conn.QueryRow(ctx, `SELECT value FROM system.metrics WHERE metric = 'QueryPreempted'`).Scan(&queuedQueries)
	if err == nil {
		metrics.QueuedQueries = queuedQueries
		metrics.QueryPreempted = queuedQueries
	}

	// Get long running query details (> 60 seconds)
	rows, err := conn.Query(ctx, `
		SELECT
			query_id,
			user,
			query,
			elapsed,
			memory_usage,
			read_rows,
			read_bytes,
			current_database,
			is_cancelled
		FROM system.processes
		WHERE elapsed > 60 AND is_cancelled = 0
		ORDER BY elapsed DESC
		LIMIT 10
	`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var q model.LongRunningQuery
			var isCancelled uint8
			var readRows, readBytes uint64

			err := rows.Scan(
				&q.QueryID,
				&q.User,
				&q.Query,
				&q.ElapsedTime,
				&q.MemoryUsage,
				&readRows,
				&readBytes,
				&q.Database,
				&isCancelled,
			)
			if err != nil {
				continue
			}
			q.ReadRows = int64(readRows)
			q.ReadBytes = int64(readBytes)
			q.IsCancelled = isCancelled == 1

			// Truncate query if too long
			if len(q.Query) > 500 {
				q.Query = q.Query[:500] + "..."
			}

			metrics.LongRunningQueryDetails = append(metrics.LongRunningQueryDetails, q)
		}
	}

	return metrics, nil
}

// CollectClickHouseErrors collects error signals from system.errors
func (c *ClickhouseCollector) CollectClickHouseErrors() ([]model.ClickHouseError, error) {
	conn, err := c.GetConnection()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	errors := make([]model.ClickHouseError, 0)

	// Get recent errors from system.errors
	rows, err := conn.Query(ctx, `
		SELECT
			name,
			code,
			value,
			last_error_time,
			last_error_message
		FROM system.errors
		WHERE last_error_time > now() - INTERVAL 5 MINUTE
			OR name IN (
				'TOO_MANY_PARTS',
				'MEMORY_LIMIT_EXCEEDED',
				'CANNOT_ALLOCATE_MEMORY',
				'TIMEOUT_EXCEEDED',
				'SOCKET_TIMEOUT',
				'NETWORK_ERROR',
				'ZOOKEEPER_ERROR',
				'REPLICA_IS_ALREADY_EXIST',
				'REPLICA_IS_NOT_IN_QUORUM',
				'NO_REPLICA_HAS_PART'
			)
		ORDER BY last_error_time DESC
		LIMIT 50
	`)

	if err != nil {
		logger.Debug("Failed to get ClickHouse errors: %v", err)
		return errors, nil
	}
	defer rows.Close()

	for rows.Next() {
		var e model.ClickHouseError
		var value uint64

		err := rows.Scan(
			&e.Name,
			&e.Code,
			&value,
			&e.LastErrorTime,
			&e.LastErrorMessage,
		)
		if err != nil {
			continue
		}
		e.Value = int64(value)

		// Truncate error message if too long
		if len(e.LastErrorMessage) > 500 {
			e.LastErrorMessage = e.LastErrorMessage[:500] + "..."
		}

		errors = append(errors, e)
	}

	return errors, nil
}

// CollectMutationMetrics collects mutation operation metrics
func (c *ClickhouseCollector) CollectMutationMetrics() ([]model.MutationMetrics, error) {
	conn, err := c.GetConnection()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mutations := make([]model.MutationMetrics, 0)

	// Get incomplete mutations from system.mutations
	// Note: block_numbers_acquired_by_mutation was removed in newer ClickHouse versions
	rows, err := conn.Query(ctx, `
		SELECT
			database,
			table,
			mutation_id,
			command,
			create_time,
			parts_to_do,
			is_done,
			latest_fail_reason,
			latest_fail_time,
			dateDiff('second', create_time, now()) as age_seconds
		FROM system.mutations
		WHERE NOT is_done
		ORDER BY create_time ASC
		LIMIT 100
	`)

	if err != nil {
		logger.Debug("Failed to get mutation metrics: %v", err)
		return mutations, nil
	}
	defer rows.Close()

	for rows.Next() {
		var m model.MutationMetrics
		var partsToDo uint64
		var isDone uint8
		var latestFailTime *time.Time

		err := rows.Scan(
			&m.Database,
			&m.Table,
			&m.MutationID,
			&m.Command,
			&m.CreateTime,
			&partsToDo,
			&isDone,
			&m.LatestFailReason,
			&latestFailTime,
			&m.AgeSeconds,
		)
		if err != nil {
			continue
		}

		m.PartsToDo = int64(partsToDo)
		m.IsDone = isDone == 1
		if latestFailTime != nil {
			m.LatestFailTime = *latestFailTime
		}

		// Truncate command if too long
		if len(m.Command) > 300 {
			m.Command = m.Command[:300] + "..."
		}

		mutations = append(mutations, m)
	}

	return mutations, nil
}

// CollectReplicaMetrics collects detailed replica metrics
func (c *ClickhouseCollector) CollectReplicaMetrics() ([]model.ReplicaMetrics, error) {
	conn, err := c.GetConnection()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	replicas := make([]model.ReplicaMetrics, 0)

	// Get replica metrics from system.replicas
	rows, err := conn.Query(ctx, `
		SELECT
			database,
			table,
			is_leader,
			is_readonly,
			is_session_expired,
			absolute_delay,
			queue_size,
			inserts_in_queue,
			merges_in_queue,
			log_pointer,
			total_replicas,
			active_replicas,
			last_queue_update
		FROM system.replicas
		WHERE is_readonly = 1 OR absolute_delay > 30 OR queue_size > 0
		ORDER BY absolute_delay DESC, queue_size DESC
		LIMIT 50
	`)

	if err != nil {
		logger.Debug("Failed to get replica metrics: %v", err)
		return replicas, nil
	}
	defer rows.Close()

	for rows.Next() {
		var r model.ReplicaMetrics
		var isLeader, isReadonly, isSessionExpired uint8
		var absoluteDelay, queueSize, insertsInQueue, mergesInQueue, logPointer, totalReplicas, activeReplicas uint64

		err := rows.Scan(
			&r.Database,
			&r.Table,
			&isLeader,
			&isReadonly,
			&isSessionExpired,
			&absoluteDelay,
			&queueSize,
			&insertsInQueue,
			&mergesInQueue,
			&logPointer,
			&totalReplicas,
			&activeReplicas,
			&r.LastQueueUpdate,
		)
		if err != nil {
			continue
		}

		r.IsLeader = isLeader == 1
		r.IsReadonly = isReadonly == 1
		r.IsSessionExpired = isSessionExpired == 1
		r.AbsoluteDelay = int64(absoluteDelay)
		r.QueueSize = int64(queueSize)
		r.InsertsInQueue = int64(insertsInQueue)
		r.MergesInQueue = int64(mergesInQueue)
		r.LogPointer = int64(logPointer)
		r.TotalReplicas = int64(totalReplicas)
		r.ActiveReplicas = int64(activeReplicas)

		replicas = append(replicas, r)
	}

	return replicas, nil
}

// CollectAllAdvancedMetrics collects all advanced ClickHouse-specific metrics
func (c *ClickhouseCollector) CollectAllAdvancedMetrics() (*model.MergeMetrics, *model.PartsMetrics, *model.MemoryPressureMetrics, *model.QueryConcurrencyMetrics, []model.ClickHouseError, error) {
	mergeMetrics, err := c.CollectMergeMetrics()
	if err != nil {
		logger.Warning("Failed to collect merge metrics: %v", err)
	}

	partsMetrics, err := c.CollectPartsMetrics()
	if err != nil {
		logger.Warning("Failed to collect parts metrics: %v", err)
	}

	memoryMetrics, err := c.CollectMemoryPressureMetrics()
	if err != nil {
		logger.Warning("Failed to collect memory pressure metrics: %v", err)
	}

	queryConcurrencyMetrics, err := c.CollectQueryConcurrencyMetrics()
	if err != nil {
		logger.Warning("Failed to collect query concurrency metrics: %v", err)
	}

	errors, err := c.CollectClickHouseErrors()
	if err != nil {
		logger.Warning("Failed to collect ClickHouse errors: %v", err)
	}

	return mergeMetrics, partsMetrics, memoryMetrics, queryConcurrencyMetrics, errors, nil
}

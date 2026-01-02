package clickhouse

import (
	"context"
	"time"

	"github.com/CloudNativeWorks/clustereye-agent-clickhouse/internal/logger"
	"github.com/CloudNativeWorks/clustereye-agent-clickhouse/internal/model"
)

// CollectReplicationStatus collects cluster replication information
func (c *ClickhouseCollector) CollectReplicationStatus() (*model.ReplicationStatus, error) {
	if c.cfg.Clickhouse.Cluster == "" {
		return &model.ReplicationStatus{
			IsEnabled: false,
			Status:    "not_configured",
		}, nil
	}

	conn, err := c.GetConnection()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	status := &model.ReplicationStatus{
		IsEnabled: true,
		Replicas:  make([]model.ReplicaInfo, 0),
		ReplicationQueue: make([]model.ReplicationTask, 0),
		Status:    "healthy",
	}

	// Get replica information from system.clusters
	rows, err := conn.Query(ctx, `
		SELECT
			host_name,
			host_address,
			port,
			is_local
		FROM system.clusters
		WHERE cluster = ?
		ORDER BY shard_num, replica_num
	`, c.cfg.Clickhouse.Cluster)

	if err != nil {
		logger.Warning("Failed to query cluster replicas: %v", err)
		status.Status = "error"
		return status, nil
	}
	defer rows.Close()

	for rows.Next() {
		var replica model.ReplicaInfo
		var isLocal uint8

		err := rows.Scan(
			&replica.HostName,
			&replica.HostAddress,
			&replica.Port,
			&isLocal,
		)

		if err != nil {
			logger.Warning("Failed to scan replica row: %v", err)
			continue
		}

		replica.IsLocal = isLocal == 1
		replica.Status = "active"
		replica.LastChecked = time.Now()

		// Get queue size for this replica if it's local
		if replica.IsLocal {
			var queueSize int64
			err = conn.QueryRow(ctx, `
				SELECT count()
				FROM system.replication_queue
			`).Scan(&queueSize)

			if err != nil {
				logger.Debug("Failed to get replication queue size: %v", err)
			} else {
				replica.QueueSize = queueSize
			}

			// Get replication delay
			var delay int64
			err = conn.QueryRow(ctx, `
				SELECT max(absolute_delay)
				FROM system.replicas
			`).Scan(&delay)

			if err != nil {
				logger.Debug("Failed to get replication delay: %v", err)
			} else {
				replica.Delay = delay
				if delay > status.MaxDelay {
					status.MaxDelay = delay
				}
			}
		}

		status.Replicas = append(status.Replicas, replica)
	}

	// Get replication queue tasks
	queueRows, err := conn.Query(ctx, `
		SELECT
			database,
			table,
			type,
			create_time,
			num_tries,
			last_exception
		FROM system.replication_queue
		ORDER BY create_time DESC
		LIMIT 50
	`)

	if err != nil {
		logger.Debug("Failed to query replication queue: %v", err)
	} else {
		defer queueRows.Close()

		for queueRows.Next() {
			var task model.ReplicationTask

			err := queueRows.Scan(
				&task.Database,
				&task.Table,
				&task.TaskType,
				&task.CreateTime,
				&task.NumTries,
				&task.LastException,
			)

			if err != nil {
				logger.Warning("Failed to scan replication task: %v", err)
				continue
			}

			status.ReplicationQueue = append(status.ReplicationQueue, task)
		}
	}

	// Determine overall status
	if status.MaxDelay > int64(c.cfg.Clickhouse.ClusterMonitoring.ReplicaTimeout) {
		status.Status = "lagging"
	}

	if len(status.ReplicationQueue) > 100 {
		status.Status = "degraded"
	}

	return status, nil
}

// CollectSystemTablesData collects data from various system tables
func (c *ClickhouseCollector) CollectSystemTablesData() (*model.SystemTablesData, error) {
	conn, err := c.GetConnection()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	data := &model.SystemTablesData{
		Processes:    make([]model.ProcessInfo, 0),
		Mutations:    make([]model.MutationInfo, 0),
		Merges:       make([]model.MergeInfo, 0),
		Dictionaries: make([]model.DictionaryInfo, 0),
	}

	// Collect running processes
	processRows, err := conn.Query(ctx, `
		SELECT
			query_id,
			user,
			address,
			elapsed,
			read_rows,
			read_bytes,
			memory_usage,
			query
		FROM system.processes
		ORDER BY elapsed DESC
		LIMIT 50
	`)

	if err != nil {
		logger.Debug("Failed to query processes: %v", err)
	} else {
		defer processRows.Close()

		for processRows.Next() {
			var proc model.ProcessInfo
			var rowsRead, bytesRead uint64

			err := processRows.Scan(
				&proc.QueryID,
				&proc.User,
				&proc.Address,
				&proc.ElapsedTime,
				&rowsRead,
				&bytesRead,
				&proc.MemoryUsage,
				&proc.Query,
			)

			if err != nil {
				logger.Warning("Failed to scan process row: %v", err)
				continue
			}

			// Convert UInt64 to int64
			proc.RowsRead = int64(rowsRead)
			proc.BytesRead = int64(bytesRead)

			// Truncate query if too long
			if len(proc.Query) > 1000 {
				proc.Query = proc.Query[:1000] + "..."
			}

			data.Processes = append(data.Processes, proc)
		}
	}

	// Collect mutations
	mutationRows, err := conn.Query(ctx, `
		SELECT
			database,
			table,
			mutation_id,
			command,
			create_time,
			parts_to_do,
			is_done
		FROM system.mutations
		WHERE is_done = 0
		ORDER BY create_time DESC
		LIMIT 50
	`)

	if err != nil {
		logger.Debug("Failed to query mutations: %v", err)
	} else {
		defer mutationRows.Close()

		for mutationRows.Next() {
			var mut model.MutationInfo
			var isDone uint8

			err := mutationRows.Scan(
				&mut.Database,
				&mut.Table,
				&mut.MutationID,
				&mut.Command,
				&mut.CreateTime,
				&mut.PartsToMutate,
				&isDone,
			)

			if err != nil {
				logger.Warning("Failed to scan mutation row: %v", err)
				continue
			}

			mut.IsCompleted = isDone == 1

			data.Mutations = append(data.Mutations, mut)
		}
	}

	// Collect merges
	mergeRows, err := conn.Query(ctx, `
		SELECT
			database,
			table,
			elapsed,
			progress,
			num_parts,
			total_size_bytes_compressed,
			total_size_bytes_uncompressed
		FROM system.merges
		ORDER BY elapsed DESC
		LIMIT 50
	`)

	if err != nil {
		logger.Debug("Failed to query merges: %v", err)
	} else {
		defer mergeRows.Close()

		for mergeRows.Next() {
			var merge model.MergeInfo
			var numParts, totalSizeCompressed, totalSizeUncompressed uint64

			err := mergeRows.Scan(
				&merge.Database,
				&merge.Table,
				&merge.ElapsedTime,
				&merge.Progress,
				&numParts,
				&totalSizeCompressed,
				&totalSizeUncompressed,
			)

			if err != nil {
				logger.Warning("Failed to scan merge row: %v", err)
				continue
			}

			// Use num_parts as TotalRows and sizes as bytes
			merge.TotalRows = int64(numParts)
			merge.BytesRead = int64(totalSizeUncompressed)
			merge.BytesWritten = int64(totalSizeCompressed)

			data.Merges = append(data.Merges, merge)
		}
	}

	// Collect dictionaries
	dictRows, err := conn.Query(ctx, `
		SELECT
			name,
			database,
			status,
			loading_start_time,
			loading_duration,
			element_count
		FROM system.dictionaries
		ORDER BY name
		LIMIT 100
	`)

	if err != nil {
		logger.Debug("Failed to query dictionaries: %v", err)
	} else {
		defer dictRows.Close()

		for dictRows.Next() {
			var dict model.DictionaryInfo
			var loadStartTime *time.Time

			err := dictRows.Scan(
				&dict.Name,
				&dict.Database,
				&dict.Status,
				&loadStartTime,
				&dict.LoadDuration,
				&dict.ElementCount,
			)

			if err != nil {
				logger.Warning("Failed to scan dictionary row: %v", err)
				continue
			}

			if loadStartTime != nil {
				dict.LastLoadTime = *loadStartTime
			}

			data.Dictionaries = append(data.Dictionaries, dict)
		}
	}

	return data, nil
}

// ExecuteQuery executes a query and returns the result
func (c *ClickhouseCollector) ExecuteQuery(query string) (interface{}, error) {
	conn, err := c.GetConnection()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rows, err := conn.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Collect results
	results := make([]map[string]interface{}, 0)
	columnTypes := rows.ColumnTypes()

	for rows.Next() {
		values := make([]interface{}, len(columnTypes))
		valuePtrs := make([]interface{}, len(columnTypes))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, err
		}

		row := make(map[string]interface{})
		for i, col := range columnTypes {
			row[col.Name()] = values[i]
		}
		results = append(results, row)
	}

	return results, nil
}

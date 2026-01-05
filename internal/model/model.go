package model

import (
	"time"
)

// SystemData represents the complete data structure sent by the agent
type SystemData struct {
	AgentKey   string          `json:"agent_key"`
	AgentName  string          `json:"agent_name"`
	Timestamp  int64           `json:"timestamp"`
	Clickhouse *ClickhouseData `json:"clickhouse,omitempty"`
	System     *SystemMetrics  `json:"system,omitempty"`
}

// ClickhouseData contains all ClickHouse-specific monitoring data
type ClickhouseData struct {
	Info            *ClickhouseInfo          `json:"info"`
	Metrics         *ClickhouseMetrics       `json:"metrics"`
	Queries         []ClickhouseQuery        `json:"queries,omitempty"`
	Tables          []TableMetric            `json:"tables,omitempty"`
	Replication     *ReplicationStatus       `json:"replication,omitempty"`
	SystemTables    *SystemTablesData        `json:"system_tables,omitempty"`
	// New ClickHouse-specific metrics
	MergeMetrics    *MergeMetrics            `json:"merge_metrics,omitempty"`
	PartsMetrics    *PartsMetrics            `json:"parts_metrics,omitempty"`
	MemoryPressure  *MemoryPressureMetrics   `json:"memory_pressure,omitempty"`
	QueryConcurrency *QueryConcurrencyMetrics `json:"query_concurrency,omitempty"`
	Errors          []ClickHouseError        `json:"errors,omitempty"`
}

// ClickhouseInfo contains basic ClickHouse instance information
type ClickhouseInfo struct {
	ClusterName    string    `json:"cluster_name"`
	IP             string    `json:"ip"`
	Hostname       string    `json:"hostname"`
	NodeStatus     string    `json:"node_status"`
	Version        string    `json:"version"`
	Location       string    `json:"location"`
	Status         string    `json:"status"`
	Port           string    `json:"port"`
	Uptime         int64     `json:"uptime"`
	ConfigPath     string    `json:"config_path"`
	DataPath       string    `json:"data_path"`
	IsReplicated   bool      `json:"is_replicated"`
	ReplicaCount   int       `json:"replica_count"`
	ShardCount     int       `json:"shard_count"`
	TotalVCPU      int       `json:"total_vcpu"`
	TotalMemory    int64     `json:"total_memory"`
	LastCheckTime  time.Time `json:"last_check_time"`
}

// ClickhouseMetrics contains performance and resource metrics
type ClickhouseMetrics struct {
	// Connection metrics
	CurrentConnections int64 `json:"current_connections"`
	MaxConnections     int64 `json:"max_connections"`

	// Query metrics
	QueriesPerSecond       float64 `json:"queries_per_second"`
	SelectQueriesPerSecond float64 `json:"select_queries_per_second"`
	InsertQueriesPerSecond float64 `json:"insert_queries_per_second"`
	RunningQueries         int64   `json:"running_queries"`
	QueuedQueries          int64   `json:"queued_queries"`

	// Memory metrics
	MemoryUsage      int64   `json:"memory_usage"`
	MemoryAvailable  int64   `json:"memory_available"`
	MemoryPercent    float64 `json:"memory_percent"`

	// Disk metrics
	DiskUsage        int64   `json:"disk_usage"`
	DiskAvailable    int64   `json:"disk_available"`
	DiskPercent      float64 `json:"disk_percent"`

	// Performance metrics
	MergesInProgress int64   `json:"merges_in_progress"`
	PartsCount       int64   `json:"parts_count"`
	RowsRead         int64   `json:"rows_read"`
	BytesRead        int64   `json:"bytes_read"`

	// Network metrics
	NetworkReceiveBytes  int64 `json:"network_receive_bytes"`
	NetworkSendBytes     int64 `json:"network_send_bytes"`

	// CPU metrics
	CPUUsage         float64 `json:"cpu_usage"`

	// Cache metrics
	MarkCacheBytes   int64 `json:"mark_cache_bytes"`
	MarkCacheFiles   int64 `json:"mark_cache_files"`

	// Response time metric (SELECT 1 query latency in milliseconds)
	ResponseTimeMs   float64 `json:"response_time_ms"`

	CollectionTime   time.Time `json:"collection_time"`
}

// ClickhouseQuery represents a query execution record
type ClickhouseQuery struct {
	QueryID          string    `json:"query_id"`
	Query            string    `json:"query"`
	User             string    `json:"user"`
	QueryStartTime   time.Time `json:"query_start_time"`
	QueryDuration    float64   `json:"query_duration_ms"`
	MemoryUsage      int64     `json:"memory_usage"`
	ReadRows         int64     `json:"read_rows"`
	ReadBytes        int64     `json:"read_bytes"`
	WrittenRows      int64     `json:"written_rows"`
	WrittenBytes     int64     `json:"written_bytes"`
	ResultRows       int64     `json:"result_rows"`
	ResultBytes      int64     `json:"result_bytes"`
	QueryType        string    `json:"query_type"`
	Database         string    `json:"database"`
	Tables           []string  `json:"tables,omitempty"`
	Exception        string    `json:"exception,omitempty"`
	IsSlowQuery      bool      `json:"is_slow_query"`
}

// TableMetric contains metrics for individual tables
type TableMetric struct {
	Database         string    `json:"database"`
	Table            string    `json:"table"`
	Engine           string    `json:"engine"`
	TotalRows        int64     `json:"total_rows"`
	TotalBytes       int64     `json:"total_bytes"`
	PartsCount       int64     `json:"parts_count"`
	ActiveParts      int64     `json:"active_parts"`

	// Compression
	CompressedSize   int64     `json:"compressed_size"`
	UncompressedSize int64     `json:"uncompressed_size"`
	CompressionRatio float64   `json:"compression_ratio"`

	// Modifications
	LastModifiedTime time.Time `json:"last_modified_time"`

	// Replication (if applicable)
	IsReplicated     bool      `json:"is_replicated"`
	ReplicationDelay int64     `json:"replication_delay,omitempty"`
}

// ReplicationStatus contains cluster replication information
type ReplicationStatus struct {
	IsEnabled        bool                 `json:"is_enabled"`
	Replicas         []ReplicaInfo        `json:"replicas,omitempty"`
	ReplicationQueue []ReplicationTask    `json:"replication_queue,omitempty"`
	MaxDelay         int64                `json:"max_delay"`
	Status           string               `json:"status"`
}

// ReplicaInfo represents information about a replica
type ReplicaInfo struct {
	HostName         string    `json:"host_name"`
	HostAddress      string    `json:"host_address"`
	Port             int       `json:"port"`
	IsLocal          bool      `json:"is_local"`
	Status           string    `json:"status"`
	Delay            int64     `json:"delay"`
	QueueSize        int64     `json:"queue_size"`
	LastChecked      time.Time `json:"last_checked"`
}

// ReplicationTask represents a task in the replication queue
type ReplicationTask struct {
	Database         string    `json:"database"`
	Table            string    `json:"table"`
	TaskType         string    `json:"task_type"`
	CreateTime       time.Time `json:"create_time"`
	NumTries         int       `json:"num_tries"`
	LastException    string    `json:"last_exception,omitempty"`
}

// SystemTablesData contains data from system tables
type SystemTablesData struct {
	Processes        []ProcessInfo     `json:"processes,omitempty"`
	Mutations        []MutationInfo    `json:"mutations,omitempty"`
	Merges           []MergeInfo       `json:"merges,omitempty"`
	Dictionaries     []DictionaryInfo  `json:"dictionaries,omitempty"`
}

// ProcessInfo represents a running process/query
type ProcessInfo struct {
	QueryID          string    `json:"query_id"`
	User             string    `json:"user"`
	Address          string    `json:"address"`
	ElapsedTime      float64   `json:"elapsed_time"`
	RowsRead         int64     `json:"rows_read"`
	BytesRead        int64     `json:"bytes_read"`
	MemoryUsage      int64     `json:"memory_usage"`
	Query            string    `json:"query"`
}

// MutationInfo represents a mutation operation
type MutationInfo struct {
	Database         string    `json:"database"`
	Table            string    `json:"table"`
	MutationID       string    `json:"mutation_id"`
	Command          string    `json:"command"`
	CreateTime       time.Time `json:"create_time"`
	PartsToMutate    int64     `json:"parts_to_mutate"`
	IsCompleted      bool      `json:"is_completed"`
}

// MergeInfo represents a merge operation
type MergeInfo struct {
	Database         string    `json:"database"`
	Table            string    `json:"table"`
	ElapsedTime      float64   `json:"elapsed_time"`
	Progress         float64   `json:"progress"`
	TotalRows        int64     `json:"total_rows"`
	BytesRead        int64     `json:"bytes_read"`
	BytesWritten     int64     `json:"bytes_written"`
}

// DictionaryInfo represents a dictionary status
type DictionaryInfo struct {
	Name             string    `json:"name"`
	Database         string    `json:"database"`
	Status           string    `json:"status"`
	LoadCount        int64     `json:"load_count"`
	LastLoadTime     time.Time `json:"last_load_time,omitempty"`
	LoadDuration     float64   `json:"load_duration"`
	ElementCount     int64     `json:"element_count"`
}

// SystemMetrics contains system-level metrics
type SystemMetrics struct {
	CPUUsage        float64   `json:"cpu_usage"`
	CPUCores        int32     `json:"cpu_cores"`
	MemoryUsage     float64   `json:"memory_usage"`
	MemoryTotal     int64     `json:"memory_total"`
	MemoryAvailable int64     `json:"memory_available"`
	DiskUsage       float64   `json:"disk_usage"`
	DiskTotal       int64     `json:"disk_total"`
	DiskFree        int64     `json:"disk_free"`
	LoadAverage     []float64 `json:"load_average,omitempty"`
	Uptime          int64     `json:"uptime"`
	Timestamp       time.Time `json:"timestamp"`
}

// ConfigDiff represents a configuration change
type ConfigDiff struct {
	Parameter    string `json:"parameter"`
	OldValue     string `json:"old_value"`
	NewValue     string `json:"new_value"`
	ChangeType   string `json:"change_type"` // "added", "removed", "modified"
	Impact       string `json:"impact"`      // "high", "medium", "low"
	Section      string `json:"section"`
}

// AlarmEvent represents a monitoring alarm
type AlarmEvent struct {
	AlarmID      string                 `json:"alarm_id"`
	Name         string                 `json:"name"`
	Severity     string                 `json:"severity"` // "critical", "warning", "info"
	Message      string                 `json:"message"`
	Metric       string                 `json:"metric"`
	Value        float64                `json:"value"`
	Threshold    float64                `json:"threshold"`
	Timestamp    time.Time              `json:"timestamp"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// ============================================================================
// ClickHouse-Specific Advanced Metrics
// ============================================================================

// MergeMetrics contains metrics about ClickHouse merge operations (CRITICAL for performance)
type MergeMetrics struct {
	// Active merge counts
	ActiveMerges      int64   `json:"active_merges"`
	MutationMerges    int64   `json:"mutation_merges"`
	BackgroundMerges  int64   `json:"background_merges"`

	// Merge performance
	TotalMergeTime     float64 `json:"total_merge_time_sec"`
	MaxMergeElapsed    float64 `json:"max_merge_elapsed_sec"`
	AvgMergeElapsed    float64 `json:"avg_merge_elapsed_sec"`

	// Merge I/O
	MergeBytesRead     int64   `json:"merge_bytes_read"`
	MergeBytesWritten  int64   `json:"merge_bytes_written"`
	MergeRowsRead      int64   `json:"merge_rows_read"`
	MergeRowsWritten   int64   `json:"merge_rows_written"`

	// Derived metrics
	MergeThroughputMBs float64 `json:"merge_throughput_mb_s"`
	MergeBacklog       int64   `json:"merge_backlog"`        // Pending merges
	MergeQueueSize     int64   `json:"merge_queue_size"`

	// Per-table merge info (tables with active merges)
	TableMerges        []TableMergeInfo `json:"table_merges,omitempty"`

	CollectionTime     time.Time `json:"collection_time"`
}

// TableMergeInfo contains merge info for a specific table
type TableMergeInfo struct {
	Database        string  `json:"database"`
	Table           string  `json:"table"`
	ActiveMerges    int64   `json:"active_merges"`
	MergeProgress   float64 `json:"merge_progress"`
	BytesProcessed  int64   `json:"bytes_processed"`
	ElapsedTime     float64 `json:"elapsed_time_sec"`
	IsMutation      bool    `json:"is_mutation"`
}

// PartsMetrics contains metrics about ClickHouse parts (equivalent to PostgreSQL bloat)
type PartsMetrics struct {
	// Global part counts
	TotalParts       int64   `json:"total_parts"`
	ActiveParts      int64   `json:"active_parts"`
	InactiveParts    int64   `json:"inactive_parts"`

	// Small parts analysis (too many small parts = merge can't keep up)
	SmallPartsCount  int64   `json:"small_parts_count"`   // Parts < 10MB
	SmallPartsRatio  float64 `json:"small_parts_ratio"`   // Ratio of small parts

	// Size metrics
	TotalBytesOnDisk int64   `json:"total_bytes_on_disk"`
	TotalRows        int64   `json:"total_rows"`

	// Problem tables (too many parts)
	TablesWithTooManyParts []TablePartsInfo `json:"tables_with_too_many_parts,omitempty"`

	// Thresholds for alarming
	MaxPartsPerTable int64 `json:"max_parts_per_table"`

	CollectionTime   time.Time `json:"collection_time"`
}

// TablePartsInfo contains parts info for tables with issues
type TablePartsInfo struct {
	Database         string  `json:"database"`
	Table            string  `json:"table"`
	PartsCount       int64   `json:"parts_count"`
	ActiveParts      int64   `json:"active_parts"`
	SmallParts       int64   `json:"small_parts"`
	TotalBytes       int64   `json:"total_bytes"`
	TotalRows        int64   `json:"total_rows"`
	AvgPartSize      int64   `json:"avg_part_size"`
	Engine           string  `json:"engine"`
}

// MemoryPressureMetrics contains memory-related metrics (ClickHouse is memory-aggressive)
type MemoryPressureMetrics struct {
	// Current memory tracking
	MemoryTracking         int64 `json:"memory_tracking"`
	MemoryTrackingForMerges int64 `json:"memory_tracking_for_merges"`
	MemoryTrackingForQueries int64 `json:"memory_tracking_for_queries"`

	// Memory limits
	MaxServerMemoryUsage   int64   `json:"max_server_memory_usage"`
	MemoryUsagePercent     float64 `json:"memory_usage_percent"`

	// Memory pressure events (from system.events)
	QueryMemoryLimitExceeded  int64 `json:"query_memory_limit_exceeded"`
	MemoryOvercommitWaitTime  int64 `json:"memory_overcommit_wait_time_microsec"`

	// Memory allocation failures
	CannotAllocateMemory     int64 `json:"cannot_allocate_memory"`
	MemoryAllocateFail       int64 `json:"memory_allocate_fail"`

	// Cache memory
	MarkCacheBytes           int64 `json:"mark_cache_bytes"`
	UncompressedCacheBytes   int64 `json:"uncompressed_cache_bytes"`

	CollectionTime           time.Time `json:"collection_time"`
}

// QueryConcurrencyMetrics contains query execution metrics
type QueryConcurrencyMetrics struct {
	// Running queries
	RunningQueries       int64   `json:"running_queries"`
	RunningSelectQueries int64   `json:"running_select_queries"`
	RunningInsertQueries int64   `json:"running_insert_queries"`

	// Queue metrics
	QueuedQueries        int64   `json:"queued_queries"`
	QueryPreempted       int64   `json:"query_preempted"`

	// Long running queries
	LongRunningQueries   int64   `json:"long_running_queries"`      // > 60s
	VeryLongRunningQueries int64 `json:"very_long_running_queries"` // > 300s
	MaxQueryElapsed      float64 `json:"max_query_elapsed_sec"`

	// Resource usage by queries
	TotalQueryMemoryUsage int64  `json:"total_query_memory_usage"`
	TotalReadRows         int64  `json:"total_read_rows"`
	TotalReadBytes        int64  `json:"total_read_bytes"`

	// Query details for long running ones
	LongRunningQueryDetails []LongRunningQuery `json:"long_running_query_details,omitempty"`

	CollectionTime       time.Time `json:"collection_time"`
}

// LongRunningQuery contains details about a long-running query
type LongRunningQuery struct {
	QueryID      string  `json:"query_id"`
	User         string  `json:"user"`
	Query        string  `json:"query"`
	ElapsedTime  float64 `json:"elapsed_time_sec"`
	MemoryUsage  int64   `json:"memory_usage"`
	ReadRows     int64   `json:"read_rows"`
	ReadBytes    int64   `json:"read_bytes"`
	Database     string  `json:"database"`
	IsCancelled  bool    `json:"is_cancelled"`
}

// ClickHouseError represents an error from system.errors
type ClickHouseError struct {
	Name            string    `json:"name"`
	Code            int64     `json:"code"`
	Value           int64     `json:"value"`             // Error count
	LastErrorTime   time.Time `json:"last_error_time"`
	LastErrorMessage string   `json:"last_error_message"`
	RemoteHosts     string    `json:"remote_hosts,omitempty"`
}

// ReplicaMetrics contains detailed replica metrics (enhanced from existing)
type ReplicaMetrics struct {
	Database          string    `json:"database"`
	Table             string    `json:"table"`
	IsLeader          bool      `json:"is_leader"`
	IsReadonly        bool      `json:"is_readonly"`
	IsSessionExpired  bool      `json:"is_session_expired"`
	AbsoluteDelay     int64     `json:"absolute_delay"`
	QueueSize         int64     `json:"queue_size"`
	InsertsInQueue    int64     `json:"inserts_in_queue"`
	MergesInQueue     int64     `json:"merges_in_queue"`
	LogPointer        int64     `json:"log_pointer"`
	TotalReplicas     int64     `json:"total_replicas"`
	ActiveReplicas    int64     `json:"active_replicas"`
	LastQueueUpdate   time.Time `json:"last_queue_update"`
}

// MutationMetrics contains detailed mutation metrics
type MutationMetrics struct {
	Database           string    `json:"database"`
	Table              string    `json:"table"`
	MutationID         string    `json:"mutation_id"`
	Command            string    `json:"command"`
	CreateTime         time.Time `json:"create_time"`
	BlockNumbersAcquired int64   `json:"block_numbers_acquired"`
	PartsToDo          int64     `json:"parts_to_do"`
	IsDone             bool      `json:"is_done"`
	LatestFailReason   string    `json:"latest_fail_reason,omitempty"`
	LatestFailTime     time.Time `json:"latest_fail_time,omitempty"`
	// Calculated field
	AgeSeconds         float64   `json:"age_seconds"`
}

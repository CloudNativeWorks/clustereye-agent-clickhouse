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
	Info            *ClickhouseInfo     `json:"info"`
	Metrics         *ClickhouseMetrics  `json:"metrics"`
	Queries         []ClickhouseQuery   `json:"queries,omitempty"`
	Tables          []TableMetric       `json:"tables,omitempty"`
	Replication     *ReplicationStatus  `json:"replication,omitempty"`
	SystemTables    *SystemTablesData   `json:"system_tables,omitempty"`
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

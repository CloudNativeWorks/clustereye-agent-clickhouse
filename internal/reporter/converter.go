package reporter

import (
	"time"

	"github.com/CloudNativeWorks/clustereye-agent-clickhouse/internal/model"
	pb "github.com/CloudNativeWorks/clustereye-api/pkg/agent"
)

// ConvertSystemMetrics converts internal SystemMetrics to protobuf SystemMetrics
func ConvertSystemMetrics(metrics *model.SystemMetrics) *pb.SystemMetrics {
	if metrics == nil {
		return nil
	}

	pbMetrics := &pb.SystemMetrics{
		CpuUsage:     metrics.CPUUsage,
		CpuCores:     metrics.CPUCores,
		MemoryUsage:  metrics.MemoryUsage,
		TotalMemory:  metrics.MemoryTotal,
		FreeMemory:   metrics.MemoryAvailable,
		TotalDisk:    metrics.DiskTotal,
		FreeDisk:     metrics.DiskFree,
		Uptime:       metrics.Uptime,
	}

	// Load averages
	if len(metrics.LoadAverage) >= 3 {
		pbMetrics.LoadAverage_1M = metrics.LoadAverage[0]
		pbMetrics.LoadAverage_5M = metrics.LoadAverage[1]
		pbMetrics.LoadAverage_15M = metrics.LoadAverage[2]
	}

	return pbMetrics
}

// ConvertClickhouseInfoToMetrics converts ClickHouse info to generic metrics
func ConvertClickhouseInfoToMetrics(agentID string, info *model.ClickhouseInfo) *pb.MetricBatch {
	if info == nil {
		return nil
	}

	batch := &pb.MetricBatch{
		AgentId:             agentID,
		MetricType:          "clickhouse_info",
		CollectionTimestamp: time.Now().UnixNano(),
		Metadata: map[string]string{
			"cluster_name": info.ClusterName,
			"hostname":     info.Hostname,
			"version":      info.Version,
			"location":     info.Location,
		},
	}

	metrics := make([]*pb.Metric, 0)

	// Basic info metrics
	metrics = append(metrics,
		createStringMetric("clickhouse.info.version", info.Version, info.Hostname),
		createStringMetric("clickhouse.info.status", info.Status, info.Hostname),
		createStringMetric("clickhouse.info.node_status", info.NodeStatus, info.Hostname),
		createIntMetric("clickhouse.info.uptime", info.Uptime, "seconds", info.Hostname),
		createIntMetric("clickhouse.info.shard_count", int64(info.ShardCount), "count", info.Hostname),
		createIntMetric("clickhouse.info.replica_count", int64(info.ReplicaCount), "count", info.Hostname),
		createBoolMetric("clickhouse.info.is_replicated", info.IsReplicated, info.Hostname),
	)

	batch.Metrics = metrics
	return batch
}

// ConvertClickhouseMetricsToMetrics converts ClickHouse metrics to generic metrics
func ConvertClickhouseMetricsToMetrics(agentID string, metrics *model.ClickhouseMetrics) *pb.MetricBatch {
	if metrics == nil {
		return nil
	}

	batch := &pb.MetricBatch{
		AgentId:             agentID,
		MetricType:          "clickhouse_metrics",
		CollectionTimestamp: metrics.CollectionTime.UnixNano(),
	}

	pbMetrics := make([]*pb.Metric, 0)

	// Connection metrics
	pbMetrics = append(pbMetrics,
		createIntMetric("clickhouse.connections.current", metrics.CurrentConnections, "connections", ""),
		createIntMetric("clickhouse.connections.max", metrics.MaxConnections, "connections", ""),
	)

	// Query metrics
	pbMetrics = append(pbMetrics,
		createDoubleMetric("clickhouse.queries.per_second", metrics.QueriesPerSecond, "qps", ""),
		createDoubleMetric("clickhouse.queries.select_per_second", metrics.SelectQueriesPerSecond, "qps", ""),
		createDoubleMetric("clickhouse.queries.insert_per_second", metrics.InsertQueriesPerSecond, "qps", ""),
		createIntMetric("clickhouse.queries.running", metrics.RunningQueries, "queries", ""),
		createIntMetric("clickhouse.queries.queued", metrics.QueuedQueries, "queries", ""),
	)

	// Memory metrics
	pbMetrics = append(pbMetrics,
		createIntMetric("clickhouse.memory.usage", metrics.MemoryUsage, "bytes", ""),
		createIntMetric("clickhouse.memory.available", metrics.MemoryAvailable, "bytes", ""),
		createDoubleMetric("clickhouse.memory.percent", metrics.MemoryPercent, "percent", ""),
	)

	// Disk metrics
	pbMetrics = append(pbMetrics,
		createIntMetric("clickhouse.disk.usage", metrics.DiskUsage, "bytes", ""),
		createIntMetric("clickhouse.disk.available", metrics.DiskAvailable, "bytes", ""),
		createDoubleMetric("clickhouse.disk.percent", metrics.DiskPercent, "percent", ""),
	)

	// Performance metrics
	pbMetrics = append(pbMetrics,
		createIntMetric("clickhouse.merges.in_progress", metrics.MergesInProgress, "merges", ""),
		createIntMetric("clickhouse.parts.count", metrics.PartsCount, "parts", ""),
		createIntMetric("clickhouse.rows.read", metrics.RowsRead, "rows", ""),
		createIntMetric("clickhouse.bytes.read", metrics.BytesRead, "bytes", ""),
	)

	// Network metrics
	pbMetrics = append(pbMetrics,
		createIntMetric("clickhouse.network.receive_bytes", metrics.NetworkReceiveBytes, "bytes", ""),
		createIntMetric("clickhouse.network.send_bytes", metrics.NetworkSendBytes, "bytes", ""),
	)

	// CPU and cache metrics
	pbMetrics = append(pbMetrics,
		createDoubleMetric("clickhouse.cpu.usage", metrics.CPUUsage, "percent", ""),
		createIntMetric("clickhouse.cache.mark_bytes", metrics.MarkCacheBytes, "bytes", ""),
		createIntMetric("clickhouse.cache.mark_files", metrics.MarkCacheFiles, "files", ""),
	)

	batch.Metrics = pbMetrics
	return batch
}

// Helper functions to create metrics
func createStringMetric(name, value, hostname string) *pb.Metric {
	metric := &pb.Metric{
		Name:      name,
		Timestamp: time.Now().UnixNano(),
		Value: &pb.MetricValue{
			Value: &pb.MetricValue_StringValue{StringValue: value},
		},
	}

	if hostname != "" {
		metric.Tags = []*pb.MetricTag{
			{Key: "hostname", Value: hostname},
		}
	}

	return metric
}

func createIntMetric(name string, value int64, unit, hostname string) *pb.Metric {
	metric := &pb.Metric{
		Name:      name,
		Timestamp: time.Now().UnixNano(),
		Unit:      unit,
		Value: &pb.MetricValue{
			Value: &pb.MetricValue_IntValue{IntValue: value},
		},
	}

	if hostname != "" {
		metric.Tags = []*pb.MetricTag{
			{Key: "hostname", Value: hostname},
		}
	}

	return metric
}

func createDoubleMetric(name string, value float64, unit, hostname string) *pb.Metric {
	metric := &pb.Metric{
		Name:      name,
		Timestamp: time.Now().UnixNano(),
		Unit:      unit,
		Value: &pb.MetricValue{
			Value: &pb.MetricValue_DoubleValue{DoubleValue: value},
		},
	}

	if hostname != "" {
		metric.Tags = []*pb.MetricTag{
			{Key: "hostname", Value: hostname},
		}
	}

	return metric
}

func createBoolMetric(name string, value bool, hostname string) *pb.Metric {
	metric := &pb.Metric{
		Name:      name,
		Timestamp: time.Now().UnixNano(),
		Value: &pb.MetricValue{
			Value: &pb.MetricValue_BoolValue{BoolValue: value},
		},
	}

	if hostname != "" {
		metric.Tags = []*pb.MetricTag{
			{Key: "hostname", Value: hostname},
		}
	}

	return metric
}

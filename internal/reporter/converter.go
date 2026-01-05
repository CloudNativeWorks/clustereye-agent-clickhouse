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

	// Response time metric
	pbMetrics = append(pbMetrics,
		createDoubleMetric("clickhouse.response_time_ms", metrics.ResponseTimeMs, "ms", ""),
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

// ConvertNetworkMetricsToMetricBatch converts network metrics to a MetricBatch
func ConvertNetworkMetricsToMetricBatch(agentID string, networkMetrics []model.NetworkInterfaceMetrics) *pb.MetricBatch {
	if len(networkMetrics) == 0 {
		return nil
	}

	batch := &pb.MetricBatch{
		AgentId:             agentID,
		MetricType:          "system_network",
		CollectionTimestamp: time.Now().UnixNano(),
	}

	metrics := make([]*pb.Metric, 0)

	for _, net := range networkMetrics {
		iface := net.Interface

		// Throughput metrics
		metrics = append(metrics,
			createDoubleMetricWithInterface("system.network.throughput_sent_mbps", net.ThroughputSentMbps, "mbps", iface),
			createDoubleMetricWithInterface("system.network.throughput_received_mbps", net.ThroughputRecvMbps, "mbps", iface),
		)

		// Bytes metrics
		metrics = append(metrics,
			createUint64MetricWithInterface("system.network.bytes_sent", net.BytesSent, "bytes", iface),
			createUint64MetricWithInterface("system.network.bytes_received", net.BytesReceived, "bytes", iface),
		)

		// Packets metrics
		metrics = append(metrics,
			createUint64MetricWithInterface("system.network.packets_sent", net.PacketsSent, "packets", iface),
			createUint64MetricWithInterface("system.network.packets_received", net.PacketsReceived, "packets", iface),
		)

		// Errors and drops
		metrics = append(metrics,
			createUint64MetricWithInterface("system.network.errors_in", net.ErrorsIn, "count", iface),
			createUint64MetricWithInterface("system.network.errors_out", net.ErrorsOut, "count", iface),
			createUint64MetricWithInterface("system.network.drops_in", net.DropsIn, "count", iface),
			createUint64MetricWithInterface("system.network.drops_out", net.DropsOut, "count", iface),
		)
	}

	batch.Metrics = metrics
	return batch
}

// ConvertDiskIOMetricsToMetricBatch converts disk I/O metrics to a MetricBatch
func ConvertDiskIOMetricsToMetricBatch(agentID string, diskMetrics []model.DiskIOMetrics) *pb.MetricBatch {
	if len(diskMetrics) == 0 {
		return nil
	}

	batch := &pb.MetricBatch{
		AgentId:             agentID,
		MetricType:          "system_disk_io",
		CollectionTimestamp: time.Now().UnixNano(),
	}

	metrics := make([]*pb.Metric, 0)

	for _, disk := range diskMetrics {
		diskName := disk.Disk

		// IOPS metrics
		metrics = append(metrics,
			createDoubleMetricWithDisk("system.disk.read_iops", disk.ReadIOPS, "iops", diskName),
			createDoubleMetricWithDisk("system.disk.write_iops", disk.WriteIOPS, "iops", diskName),
		)

		// Throughput metrics
		metrics = append(metrics,
			createDoubleMetricWithDisk("system.disk.read_throughput_mb_s", disk.ReadThroughputMBs, "mb/s", diskName),
			createDoubleMetricWithDisk("system.disk.write_throughput_mb_s", disk.WriteThroughputMBs, "mb/s", diskName),
		)

		// Bytes metrics
		metrics = append(metrics,
			createUint64MetricWithDisk("system.disk.read_bytes", disk.ReadBytes, "bytes", diskName),
			createUint64MetricWithDisk("system.disk.write_bytes", disk.WriteBytes, "bytes", diskName),
		)

		// Count metrics
		metrics = append(metrics,
			createUint64MetricWithDisk("system.disk.read_count", disk.ReadCount, "count", diskName),
			createUint64MetricWithDisk("system.disk.write_count", disk.WriteCount, "count", diskName),
		)

		// Time metrics
		metrics = append(metrics,
			createUint64MetricWithDisk("system.disk.read_time_ms", disk.ReadTime, "ms", diskName),
			createUint64MetricWithDisk("system.disk.write_time_ms", disk.WriteTime, "ms", diskName),
			createUint64MetricWithDisk("system.disk.io_time_ms", disk.IoTime, "ms", diskName),
		)
	}

	batch.Metrics = metrics
	return batch
}

// Helper functions with interface/disk tags
func createDoubleMetricWithInterface(name string, value float64, unit, iface string) *pb.Metric {
	return &pb.Metric{
		Name:      name,
		Timestamp: time.Now().UnixNano(),
		Unit:      unit,
		Value: &pb.MetricValue{
			Value: &pb.MetricValue_DoubleValue{DoubleValue: value},
		},
		Tags: []*pb.MetricTag{
			{Key: "interface", Value: iface},
		},
	}
}

func createUint64MetricWithInterface(name string, value uint64, unit, iface string) *pb.Metric {
	return &pb.Metric{
		Name:      name,
		Timestamp: time.Now().UnixNano(),
		Unit:      unit,
		Value: &pb.MetricValue{
			Value: &pb.MetricValue_IntValue{IntValue: int64(value)},
		},
		Tags: []*pb.MetricTag{
			{Key: "interface", Value: iface},
		},
	}
}

func createDoubleMetricWithDisk(name string, value float64, unit, disk string) *pb.Metric {
	return &pb.Metric{
		Name:      name,
		Timestamp: time.Now().UnixNano(),
		Unit:      unit,
		Value: &pb.MetricValue{
			Value: &pb.MetricValue_DoubleValue{DoubleValue: value},
		},
		Tags: []*pb.MetricTag{
			{Key: "disk", Value: disk},
		},
	}
}

func createUint64MetricWithDisk(name string, value uint64, unit, disk string) *pb.Metric {
	return &pb.Metric{
		Name:      name,
		Timestamp: time.Now().UnixNano(),
		Unit:      unit,
		Value: &pb.MetricValue{
			Value: &pb.MetricValue_IntValue{IntValue: int64(value)},
		},
		Tags: []*pb.MetricTag{
			{Key: "disk", Value: disk},
		},
	}
}

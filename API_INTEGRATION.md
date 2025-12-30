# ClusterEye API Integration Guide

This document describes how the ClickHouse agent integrates with the ClusterEye API.

## Overview

The ClickHouse agent uses the `clustereye-api` package (v1.0.0) for communication with the ClusterEye server via gRPC. Since the API doesn't have native ClickHouse message types yet, we use the generic metrics system to send ClickHouse-specific data.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    ClickHouse Agent                             │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌──────────────┐      ┌──────────────┐     ┌──────────────┐  │
│  │  Collector   │─────▶│  Converter   │────▶│   Reporter   │  │
│  │              │      │              │     │              │  │
│  │ - ClickHouse │      │ Model → Proto│     │ gRPC Client  │  │
│  │   Metrics    │      │              │     │              │  │
│  │ - System     │      │ - MetricBatch│     │ - Register   │  │
│  │   Metrics    │      │ - SystemData │     │ - SendData   │  │
│  └──────────────┘      └──────────────┘     │ - Heartbeat  │  │
│                                             │ - Stream     │  │
│                                             └──────┬───────┘  │
└─────────────────────────────────────────────────────│─────────┘
                                                      │
                                                      │ gRPC/Protobuf
                                                      ▼
                                            ┌─────────────────┐
                                            │ ClusterEye API  │
                                            │   (Server)      │
                                            └─────────────────┘
```

## Protocol Buffers Integration

### Dependency

```go
require github.com/CloudNativeWorks/clustereye-api v1.0.0
```

### Message Types Used

#### 1. Agent Registration

```go
// Register agent with the server
AgentInfo {
    Key:      "agent-key"
    Hostname: "server-hostname"
    Ip:       "10.0.0.1"
    Platform: "clickhouse"
    Auth:     true/false
    Test:     "connection-test-result"
}

RegisterRequest {
    AgentInfo: AgentInfo
}

RegisterResponse {
    Registration: RegistrationResult {
        Status:  "success"
        Message: "Agent registered successfully"
    }
}
```

**Implementation:** `reporter.AgentRegistration()`

#### 2. System Metrics

```go
// Send system-level metrics
SystemMetrics {
    CpuUsage:        75.5
    CpuCores:        8
    MemoryUsage:     65.2
    TotalMemory:     16GB
    FreeMemory:      5GB
    TotalDisk:       500GB
    FreeDisk:        200GB
    LoadAverage_1M:  1.5
    LoadAverage_5M:  1.2
    LoadAverage_15M: 1.0
    Uptime:          3600
}

SystemMetricsRequest {
    AgentId: "agent-key"
}

SystemMetricsResponse {
    Status:  "ok"
    Metrics: SystemMetrics
}
```

**Implementation:** `reporter.sendSystemMetrics()`

#### 3. Generic Metrics (ClickHouse Data)

Since the API doesn't have native ClickHouse messages, we use the generic `MetricBatch` system:

```go
MetricBatch {
    AgentId:             "agent-key"
    MetricType:          "clickhouse_info" | "clickhouse_metrics"
    CollectionTimestamp: 1234567890
    Metadata: {
        "cluster_name": "my_cluster"
        "hostname":     "ch-server-1"
        "version":      "23.8.1"
    }
    Metrics: [
        Metric {
            Name:      "clickhouse.connections.current"
            Value:     IntValue(150)
            Unit:      "connections"
            Timestamp: 1234567890
            Tags: [
                {Key: "hostname", Value: "ch-server-1"}
            ]
        },
        Metric {
            Name:      "clickhouse.queries.per_second"
            Value:     DoubleValue(1250.5)
            Unit:      "qps"
            Timestamp: 1234567890
        }
        // ... more metrics
    ]
}

SendMetricsRequest {
    Batch: MetricBatch
}

SendMetricsResponse {
    Status:         "ok"
    Message:        "Metrics received"
    ProcessedCount: 45
}
```

**Implementation:**
- `reporter.sendClickhouseInfo()`
- `reporter.sendClickhouseMetrics()`

#### 4. Heartbeat

```go
// Keep connection alive
Heartbeat {
    Timestamp: 1234567890
}

AgentMessage {
    Payload: Heartbeat
}
```

**Implementation:** `reporter.SendHeartbeat()`

#### 5. Bidirectional Stream

```go
// Agent to Server
AgentMessage {
    oneof Payload {
        AgentInfo agent_info
        Heartbeat heartbeat
        QueryResult query_result
        SystemMetrics system_metrics
    }
}

// Server to Agent
ServerMessage {
    oneof Payload {
        Query query
        Error error
        RegistrationResult registration
        SystemMetricsRequest metrics_request
    }
}
```

**Implementation:** `reporter.StartStream()`

## Metric Conversion

### ClickHouse Info Metrics

The converter (`converter.go`) transforms ClickHouse instance information into generic metrics:

```go
// Example conversions
model.ClickhouseInfo → MetricBatch {
    MetricType: "clickhouse_info"
    Metrics: [
        "clickhouse.info.version"        → StringValue
        "clickhouse.info.status"         → StringValue
        "clickhouse.info.uptime"         → IntValue (seconds)
        "clickhouse.info.shard_count"    → IntValue
        "clickhouse.info.replica_count"  → IntValue
        "clickhouse.info.is_replicated"  → BoolValue
    ]
}
```

### ClickHouse Performance Metrics

```go
model.ClickhouseMetrics → MetricBatch {
    MetricType: "clickhouse_metrics"
    Metrics: [
        // Connections
        "clickhouse.connections.current"          → IntValue
        "clickhouse.connections.max"              → IntValue

        // Queries
        "clickhouse.queries.per_second"           → DoubleValue
        "clickhouse.queries.select_per_second"    → DoubleValue
        "clickhouse.queries.insert_per_second"    → DoubleValue
        "clickhouse.queries.running"              → IntValue
        "clickhouse.queries.queued"               → IntValue

        // Memory
        "clickhouse.memory.usage"                 → IntValue (bytes)
        "clickhouse.memory.available"             → IntValue (bytes)
        "clickhouse.memory.percent"               → DoubleValue

        // Disk
        "clickhouse.disk.usage"                   → IntValue (bytes)
        "clickhouse.disk.available"               → IntValue (bytes)
        "clickhouse.disk.percent"                 → DoubleValue

        // Performance
        "clickhouse.merges.in_progress"           → IntValue
        "clickhouse.parts.count"                  → IntValue
        "clickhouse.rows.read"                    → IntValue
        "clickhouse.bytes.read"                   → IntValue

        // Network
        "clickhouse.network.receive_bytes"        → IntValue
        "clickhouse.network.send_bytes"           → IntValue

        // CPU & Cache
        "clickhouse.cpu.usage"                    → DoubleValue
        "clickhouse.cache.mark_bytes"             → IntValue
        "clickhouse.cache.mark_files"             → IntValue
    ]
}
```

## Data Flow

### 1. Agent Startup

```
1. Load configuration (agent.yml)
2. Initialize logger
3. Create Agent instance
4. Initialize ClickHouse collector
5. Connect to gRPC server
6. Register agent with server
   └─> AgentRegistration("success", "clickhouse")
7. Start background loops:
   - healthCheckLoop (10s interval)
   - dataCollectionLoop (60s interval)
   - heartbeatLoop (30s interval)
```

### 2. Data Collection Loop

```
Every 60 seconds (configurable):

1. collector.CollectAll()
   ├─> CollectSystemMetrics()
   │   └─> CPU, Memory, Disk, Load Average
   │
   └─> clickhouseCollector.CollectData()
       ├─> GetClickhouseInfo()
       ├─> CollectMetrics()
       ├─> CollectQueries()
       ├─> CollectTableMetrics()
       ├─> CollectReplicationStatus()
       └─> CollectSystemTablesData()

2. reporter.SendData(systemData)
   ├─> sendSystemMetrics(systemData.System)
   │   └─> gRPC: SendSystemMetrics()
   │
   ├─> sendClickhouseInfo(systemData.Clickhouse.Info)
   │   └─> gRPC: SendMetrics() with "clickhouse_info" batch
   │
   └─> sendClickhouseMetrics(systemData.Clickhouse.Metrics)
       └─> gRPC: SendMetrics() with "clickhouse_metrics" batch
```

### 3. Heartbeat Loop

```
Every 30 seconds:

1. Create Heartbeat message
2. Send via bidirectional stream
3. Keep connection alive
```

### 4. Server Message Handling

```
Continuous loop on bidirectional stream:

Receive ServerMessage →
  ├─> Query request
  │   └─> Execute ClickHouse query
  │       └─> Send QueryResult
  │
  ├─> Error message
  │   └─> Log error
  │
  ├─> Registration result
  │   └─> Update agent status
  │
  └─> Metrics request
      └─> Collect and send requested metrics
```

## gRPC Connection Details

### Connection Parameters

```go
Keepalive: {
    Time:                20 seconds
    Timeout:             10 seconds
    PermitWithoutStream: true
}

Message Sizes: {
    MaxRecv: 128 MB
    MaxSend: 128 MB
}

Retry Policy: {
    MaxRetries:      5
    MaxRetryDelay:   300 seconds
    ConnectionInterval: 5 seconds
}
```

### Connection Lifecycle

```
1. Initial Connection
   └─> grpc.Dial(server_address)
       └─> Create AgentServiceClient

2. Agent Registration
   └─> client.Register(RegisterRequest)
       └─> Receive agent ID

3. Start Bidirectional Stream
   └─> client.Connect()
       ├─> Send AgentInfo
       ├─> Start message receiver goroutine
       └─> Keep stream alive

4. Data Sending
   ├─> Unary RPC for metrics
   │   ├─> client.SendSystemMetrics()
   │   └─> client.SendMetrics()
   │
   └─> Stream for heartbeats
       └─> stream.Send(Heartbeat)

5. Graceful Shutdown
   ├─> Stop collection loops
   ├─> stream.CloseSend()
   └─> conn.Close()
```

## Error Handling

### Connection Errors

```go
// Automatic retry with exponential backoff
if err := reporter.Connect(); err != nil {
    logger.Warning("Failed to connect: %v", err)
    // Retry after ConnectionIntervalSec
}
```

### Send Errors

```go
// Graceful degradation - log and continue
if err := reporter.SendData(data); err != nil {
    logger.Warning("Failed to send data: %v", err)
    // Continue collecting, will retry next cycle
}
```

### Stream Errors

```go
// Automatic stream recreation
if err := stream.Recv(); err != nil {
    logger.Warning("Stream error: %v", err)
    // Stream will be recreated on next StartStream()
}
```

## Configuration

### Agent Configuration (agent.yml)

```yaml
key: "unique-agent-key"
name: "clickhouse-agent-1"

clickhouse:
  host: "localhost"
  port: "9000"
  user: "default"
  pass: ""
  database: "default"
  cluster: "my_cluster"
  auth: false

grpc:
  server_address: "clustereye-server:50051"
  tls_enabled: false
  connection_interval_sec: 5
  health_check_interval_sec: 10
  max_retries: 5
```

## Future Enhancements

### Native ClickHouse Proto Messages

To add native ClickHouse support to the API, the following messages should be added to `agent.proto`:

```protobuf
// ClickHouse instance information
message ClickhouseInfo {
  string cluster_name = 1;
  string ip = 2;
  string hostname = 3;
  string node_status = 4;
  string version = 5;
  string location = 6;
  string status = 7;
  string port = 8;
  int64 uptime = 9;
  string config_path = 10;
  string data_path = 11;
  bool is_replicated = 12;
  int32 replica_count = 13;
  int32 shard_count = 14;
  int32 total_vcpu = 15;
  int64 total_memory = 16;
}

// ClickHouse performance metrics
message ClickhouseMetrics {
  int64 current_connections = 1;
  int64 max_connections = 2;
  double queries_per_second = 3;
  double select_queries_per_second = 4;
  double insert_queries_per_second = 5;
  int64 running_queries = 6;
  int64 queued_queries = 7;
  int64 memory_usage = 8;
  int64 memory_available = 9;
  double memory_percent = 10;
  int64 disk_usage = 11;
  int64 disk_available = 12;
  double disk_percent = 13;
  int64 merges_in_progress = 14;
  int64 parts_count = 15;
  int64 rows_read = 16;
  int64 bytes_read = 17;
  int64 network_receive_bytes = 18;
  int64 network_send_bytes = 19;
  double cpu_usage = 20;
  int64 mark_cache_bytes = 21;
  int64 mark_cache_files = 22;
  int64 collection_timestamp = 23;
}

// Service methods
service AgentService {
  // ... existing methods

  // ClickHouse-specific methods
  rpc SendClickhouseInfo(ClickhouseInfoRequest) returns (ClickhouseInfoResponse);
}

message ClickhouseInfoRequest {
  ClickhouseInfo clickhouse_info = 1;
}

message ClickhouseInfoResponse {
  string status = 1;
}
```

### Additional Features

1. **Query Execution**
   - Implement query execution via server commands
   - Return results in structured format

2. **Job Management**
   - OPTIMIZE TABLE operations
   - MUTATION monitoring
   - BACKUP/RESTORE operations

3. **Cluster Management**
   - Shard balancing
   - Replica synchronization
   - Cluster health checks

4. **Advanced Metrics**
   - Per-table query statistics
   - Replication lag details
   - Merge queue analysis

## Testing

### Unit Tests

```bash
# Test converter functions
go test ./internal/reporter -v -run TestConvert

# Test reporter methods
go test ./internal/reporter -v -run TestReporter
```

### Integration Tests

```bash
# Start test server
docker-compose up -d clustereye-api

# Run agent
./bin/clustereye-agent-clickhouse

# Verify metrics in API logs
docker-compose logs -f clustereye-api
```

## Troubleshooting

### Connection Issues

```bash
# Check gRPC server connectivity
telnet clustereye-server 50051

# Enable debug logging
export LOG_LEVEL=debug
./bin/clustereye-agent-clickhouse
```

### Metric Sending Issues

```bash
# Check metric conversion
# Add debug logs in converter.go

# Verify protobuf serialization
# Check API server logs for received metrics
```

### Stream Issues

```bash
# Monitor stream health
# Check heartbeat logs

# Verify bidirectional communication
# Look for "Received message from server" logs
```

## References

- [ClusterEye API Repository](https://github.com/CloudNativeWorks/clustereye-api)
- [Protocol Buffers Documentation](https://protobuf.dev/)
- [gRPC Go Documentation](https://grpc.io/docs/languages/go/)
- [ClickHouse Documentation](https://clickhouse.com/docs)

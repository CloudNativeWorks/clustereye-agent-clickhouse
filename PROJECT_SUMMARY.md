# ClusterEye Agent for ClickHouse - Project Summary

## Overview

This project is a complete implementation of a monitoring and management agent for ClickHouse databases, following the same architecture and patterns as the existing clustereye-agent project (which supports PostgreSQL, MSSQL, and MongoDB).

## Project Structure

```
clustereye-agent-clickhouse/
├── main.go                              # Main entry point
├── go.mod                               # Go module dependencies
├── go.sum                               # Dependency checksums
├── agent.yml.example                    # Example configuration
├── Makefile                             # Build automation
├── Dockerfile                           # Docker image definition
├── docker-compose.yml                   # Docker Compose setup
├── README.md                            # Comprehensive documentation
├── .gitignore                           # Git ignore rules
│
├── internal/
│   ├── agent/
│   │   └── agent.go                    # Agent orchestration and lifecycle
│   │
│   ├── config/
│   │   └── config.go                   # Configuration management
│   │
│   ├── logger/
│   │   ├── logger.go                   # Multi-level logging
│   │   ├── eventlog_windows.go         # Windows Event Log support
│   │   └── eventlog_other.go           # Unix syslog support
│   │
│   ├── model/
│   │   └── model.go                    # Data structures and models
│   │
│   ├── collector/
│   │   ├── collector.go                # Main data collector orchestrator
│   │   └── clickhouse/
│   │       ├── collector.go            # ClickHouse collector core
│   │       ├── metrics.go              # Metrics collection
│   │       └── replication.go          # Replication status monitoring
│   │
│   └── reporter/
│       └── reporter.go                 # gRPC communication (stub)
│
└── bin/                                 # Build output directory
```

## Key Features Implemented

### 1. Configuration Management (`internal/config/`)
- YAML-based configuration
- Auto-detection of config file locations (platform-aware)
- Default configuration generation
- Comprehensive validation
- Support for all ClickHouse connection parameters
- Query monitoring settings
- Cluster monitoring settings
- gRPC communication settings
- Logging configuration
- Configuration drift detection settings
- Profiling options

### 2. Logging System (`internal/logger/`)
- Multi-level logging (DEBUG, INFO, WARNING, ERROR, FATAL)
- Multiple output targets:
  - Console (stdout/stderr)
  - File logging with automatic directory creation
  - Windows Event Log (Windows only)
  - Syslog (Unix-like systems)
- Platform-aware log file locations
- Configurable log levels via config or environment variable

### 3. Data Models (`internal/model/`)
Comprehensive data structures for:
- **ClickhouseInfo**: Instance information (version, hostname, uptime, cluster details)
- **ClickhouseMetrics**: Performance metrics (connections, queries, memory, disk, CPU)
- **ClickhouseQuery**: Query execution records with performance data
- **TableMetric**: Per-table statistics (size, parts, compression)
- **ReplicationStatus**: Cluster and replica health monitoring
- **SystemTablesData**: Process, mutation, merge, and dictionary information
- **SystemMetrics**: OS-level metrics (CPU, memory, disk, load average)
- **ConfigDiff**: Configuration change tracking
- **AlarmEvent**: Monitoring alarm events

### 4. ClickHouse Collector (`internal/collector/clickhouse/`)

#### Core Functionality:
- Connection management with retry logic
- Health checks and automatic recovery
- Thread-safe global singleton pattern
- Connection pooling
- TLS support
- Configurable collection intervals

#### Metrics Collection:
- **Connection Metrics**: Current/max connections
- **Query Metrics**: QPS, running queries, query types
- **Memory Metrics**: Usage, availability, percentages
- **Disk Metrics**: Usage, I/O, availability
- **Performance Metrics**: Merges, parts count, reads/writes
- **Network Metrics**: Bytes sent/received
- **CPU Metrics**: Usage tracking
- **Cache Metrics**: Mark cache statistics

#### Advanced Features:
- **Query Performance Tracking**:
  - Slow query detection
  - Query execution statistics
  - Resource usage per query
  - Query text truncation for large queries

- **Table Metrics**:
  - Row counts and sizes
  - Parts statistics
  - Compression ratios
  - Modification timestamps
  - Replication status per table

- **Cluster Monitoring**:
  - Replica information and status
  - Replication lag tracking
  - Replication queue monitoring
  - Shard and replica counts

- **System Tables Integration**:
  - Running processes
  - Mutations tracking
  - Merge operations
  - Dictionary status

#### Query Execution:
- Generic query execution capability
- Result marshaling to generic structures
- Timeout handling

### 5. Main Collector Orchestrator (`internal/collector/`)
- Multi-source data aggregation
- System metrics collection using gopsutil:
  - CPU usage and core count
  - Memory usage and availability
  - Disk usage for data partition
  - Load averages
  - System uptime
- Health check coordination
- Data collection scheduling

### 6. Reporter (gRPC Communication) (`internal/reporter/`)
- gRPC connection management
- Keepalive configuration
- Large message support (128MB)
- TLS support (stub)
- Connection retry logic
- Heartbeat mechanism
- Agent registration
- Bidirectional streaming (stub for future implementation)

### 7. Agent Orchestration (`internal/agent/`)
- Application lifecycle management
- Background task coordination:
  - Health check loop
  - Data collection loop
  - Heartbeat loop
- Graceful shutdown handling
- Signal handling (SIGTERM, SIGINT)
- Optional profiling server (pprof)
- Configurable collection intervals

### 8. Main Entry Point (`main.go`)
- Command-line flag parsing
- Version information display
- Configuration loading
- Logger initialization
- Agent startup and lifecycle management

## Build System

### Makefile Targets:
- `make build`: Build for current platform
- `make build-all`: Build for all platforms (Linux, macOS, Windows)
- `make test`: Run tests
- `make test-coverage`: Run tests with coverage report
- `make run`: Build and run
- `make clean`: Clean build artifacts
- `make install`: Install to /usr/local/bin
- `make docker-build`: Build Docker image
- `make config`: Generate default configuration

### Docker Support:
- Multi-stage Dockerfile for minimal image size
- Alpine-based runtime
- Non-root user execution
- Health checks
- Volume support for configuration and logs
- Docker Compose setup with ClickHouse test instance

## Configuration

### Agent Configuration (agent.yml):
```yaml
# Agent identification
key: "unique-agent-key"
name: "agent-name"

# ClickHouse connection
clickhouse:
  host: "localhost"
  port: "9000"
  user: "default"
  pass: ""
  database: "default"
  cluster: ""
  location: "datacenter-1"
  auth: false
  tls_enabled: false

  # Query monitoring
  query_monitoring:
    enabled: true
    collection_interval_sec: 60
    max_queries_per_collection: 100
    slow_query_threshold_ms: 1000
    cpu_threshold_percent: 80.0
    memory_limit_mb: 1024
    max_query_text_length: 4000

  # Cluster monitoring
  cluster_monitoring:
    enabled: true
    check_interval_sec: 30
    replica_timeout: 10

# Server communication
grpc:
  server_address: "server:50051"
  tls_enabled: false
  connection_interval_sec: 5
  health_check_interval_sec: 10
  max_retries: 5

# Logging
logging:
  level: "info"
  use_event_log: false
  use_file_log: true

# Configuration drift detection
config_drift:
  enabled: 1
  watch_interval_sec: 30
  clickhouse_config_paths:
    - "/etc/clickhouse-server/config.xml"

# Profiling
profiling:
  enabled: false
  port: 6060
```

## Architecture Patterns Followed

This implementation follows the exact same patterns as clustereye-agent:

1. **Global Singleton Pattern**: Thread-safe collector instances
2. **Health-Aware Collection**: Skip collection when unhealthy
3. **Startup Recovery**: Retry logic during initialization
4. **Platform-Specific Code**: Build tags for Windows vs Unix
5. **Comprehensive Error Handling**: Graceful degradation
6. **Configurable Intervals**: All timing is configurable
7. **Structured Logging**: Multi-level, multi-output logging
8. **Clean Shutdown**: Proper resource cleanup on exit

## Dependencies

- **github.com/ClickHouse/clickhouse-go/v2**: ClickHouse driver
- **github.com/shirou/gopsutil/v3**: System metrics collection
- **google.golang.org/grpc**: gRPC communication
- **google.golang.org/protobuf**: Protocol buffers
- **gopkg.in/yaml.v2**: YAML parsing
- **github.com/fsnotify/fsnotify**: File system monitoring
- **github.com/kardianos/service**: Service management

## Testing

Build and run tests:
```bash
# Build
make build

# Run tests
make test

# Test with coverage
make test-coverage

# Test version
./bin/clustereye-agent-clickhouse -version

# Test with Docker
docker-compose up -d
docker-compose logs -f agent
```

## Next Steps for Full Integration

To complete the integration with the ClusterEye server:

1. **Protocol Buffers**:
   - Add ClickHouse-specific messages to `agent.proto`
   - Generate Go code from proto definitions
   - Update reporter to use actual protobuf messages

2. **Reporter Implementation**:
   - Implement actual gRPC stream handling
   - Add message serialization/deserialization
   - Implement command processing from server

3. **Configuration Drift Detection**:
   - Implement ClickHouse config file watcher
   - Add XML parsing for ClickHouse configs
   - Implement diff generation and reporting

4. **Alarm System**:
   - Implement alarm condition monitoring
   - Add threshold-based alerting
   - Integrate with reporter

5. **Job Executor**:
   - Implement OPTIMIZE TABLE operations
   - Add mutation monitoring
   - Support cluster operations

6. **Authentication & Security**:
   - Implement TLS certificate handling
   - Add secure credential storage
   - Implement authentication with server

## Status

✅ **Completed**:
- Project structure
- Configuration management
- Logging system
- Data models
- ClickHouse collector (core functionality)
- Metrics collection
- Query monitoring
- Table metrics
- Replication monitoring
- System tables data collection
- System metrics collection
- Agent orchestration
- Main entry point
- Build system
- Docker support
- Documentation

🚧 **Stubs (Ready for Implementation)**:
- gRPC protobuf integration (reporter uses stubs)
- Configuration drift watcher
- Alarm monitoring system
- Job executor

The project is fully functional for local data collection and can be enhanced with server integration by implementing the protocol buffer definitions and completing the reporter implementation.

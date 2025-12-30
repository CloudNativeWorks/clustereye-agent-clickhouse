# ClusterEye Agent for ClickHouse

ClusterEye Agent for ClickHouse is a monitoring and management agent designed specifically for ClickHouse databases. It collects performance metrics, query statistics, replication status, and system information, then reports them to the ClusterEye central management server.

## Features

- **Real-time Monitoring**: Collects comprehensive ClickHouse metrics including queries, connections, memory usage, and disk I/O
- **Query Performance Tracking**: Monitors slow queries and execution statistics
- **Cluster & Replication Monitoring**: Tracks cluster health, replica status, and replication lag
- **Table Metrics**: Collects per-table statistics including size, compression ratio, and parts count
- **System Integration**: Monitors CPU, memory, disk, and network at the OS level
- **Configuration Drift Detection**: Detects and reports changes to ClickHouse configuration files
- **Health Checks**: Continuous health monitoring with automatic recovery
- **gRPC Communication**: Efficient bidirectional streaming with the ClusterEye server
- **Cross-Platform**: Supports Linux, macOS, and Windows

## Architecture

This agent follows the same architecture as the clustereye-agent project:

```
clustereye-agent-clickhouse/
├── main.go                          # Main entry point
├── internal/
│   ├── agent/                       # Agent orchestration
│   ├── config/                      # Configuration management
│   ├── logger/                      # Multi-level logging (file, syslog, event log)
│   ├── model/                       # Data models
│   ├── collector/                   # Data collection
│   │   ├── collector.go            # Main orchestrator
│   │   └── clickhouse/             # ClickHouse-specific collectors
│   │       ├── collector.go        # Main ClickHouse collector
│   │       ├── metrics.go          # Metrics collection
│   │       └── replication.go      # Replication status
│   └── reporter/                    # gRPC communication
└── agent.yml                        # Configuration file
```

## Installation

### Prerequisites

- Go 1.21 or higher
- ClickHouse server (supported versions: 20.x, 21.x, 22.x, 23.x+)
- Access to a ClusterEye server

### Building from Source

```bash
# Clone the repository
git clone https://github.com/CloudNativeWorks/clustereye-agent-clickhouse.git
cd clustereye-agent-clickhouse

# Download dependencies
go mod download

# Build the agent
go build -o clustereye-agent-clickhouse main.go

# Or use make (if Makefile is provided)
make build
```

### Cross-Platform Builds

```bash
# Linux (AMD64)
GOOS=linux GOARCH=amd64 go build -o clustereye-agent-clickhouse-linux-amd64 main.go

# Linux (ARM64)
GOOS=linux GOARCH=arm64 go build -o clustereye-agent-clickhouse-linux-arm64 main.go

# Windows
GOOS=windows GOARCH=amd64 go build -o clustereye-agent-clickhouse.exe main.go

# macOS (Intel)
GOOS=darwin GOARCH=amd64 go build -o clustereye-agent-clickhouse-darwin-amd64 main.go

# macOS (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -o clustereye-agent-clickhouse-darwin-arm64 main.go
```

## Configuration

### Configuration File

Copy the example configuration and edit it:

```bash
cp agent.yml.example agent.yml
```

Edit `agent.yml`:

```yaml
# Agent identification
key: "your-agent-key-here"
name: "clickhouse-agent-1"

# ClickHouse database configuration
clickhouse:
  host: "localhost"
  port: "9000"
  user: "default"
  pass: ""
  database: "default"
  cluster: "my_cluster"  # Optional: for cluster monitoring
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

# gRPC server configuration
grpc:
  server_address: "localhost:50051"
  tls_enabled: false
  connection_interval_sec: 5
  health_check_interval_sec: 10

# Logging
logging:
  level: "info"  # debug, info, warning, error, fatal
  use_event_log: false  # Windows Event Log
  use_file_log: true

# Configuration drift detection
config_drift:
  enabled: 1
  watch_interval_sec: 30
  clickhouse_config_paths:
    - "/etc/clickhouse-server/config.xml"
    - "/etc/clickhouse-server/users.xml"

# Profiling (for debugging)
profiling:
  enabled: false
  port: 6060
```

### Configuration Locations

The agent searches for `agent.yml` in the following locations:

**Linux/macOS:**
- `./agent.yml` (current directory)
- `/etc/clustereye/agent.yml`
- `~/.clustereye/agent.yml`

**Windows:**
- `.\agent.yml` (current directory)
- `C:\Clustereye\agent.yml`
- Same directory as the executable

## Usage

### Running the Agent

```bash
# Run with auto-detected configuration
./clustereye-agent-clickhouse

# Run with specific configuration file
./clustereye-agent-clickhouse -config /path/to/agent.yml

# Show version information
./clustereye-agent-clickhouse -version
```

### Running as a Service

#### Linux (systemd)

Create `/etc/systemd/system/clustereye-agent.service`:

```ini
[Unit]
Description=ClusterEye Agent for ClickHouse
After=network.target clickhouse-server.service

[Service]
Type=simple
User=clustereye
ExecStart=/usr/local/bin/clustereye-agent-clickhouse -config /etc/clustereye/agent.yml
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

Enable and start:

```bash
sudo systemctl daemon-reload
sudo systemctl enable clustereye-agent
sudo systemctl start clustereye-agent
sudo systemctl status clustereye-agent
```

#### Windows (Service)

Use tools like NSSM (Non-Sucking Service Manager) or create a Windows service wrapper.

### Environment Variables

- `LOG_LEVEL`: Override logging level (debug, info, warning, error, fatal)

## Monitoring Capabilities

### Metrics Collected

**ClickHouse Metrics:**
- Connection count (current/max)
- Queries per second (total, SELECT, INSERT)
- Running and queued queries
- Memory usage and availability
- Disk usage and I/O
- Parts count and merges in progress
- Cache statistics (mark cache, uncompressed cache)
- Network I/O

**Query Statistics:**
- Query ID, text, and type
- User and database
- Execution time and resource usage
- Rows/bytes read and written
- Slow query identification

**Table Metrics:**
- Row count and size
- Parts count (active/total)
- Compression ratio
- Last modification time
- Replication status (for replicated tables)

**Replication Status:**
- Replica information (host, port, status)
- Replication lag and queue size
- Replication tasks and errors

**System Metrics:**
- CPU usage and core count
- Memory usage and availability
- Disk usage and availability
- Load average
- System uptime

## Troubleshooting

### Agent Won't Start

1. Check configuration file syntax
2. Verify ClickHouse connection settings
3. Check log files for errors
4. Ensure gRPC server is accessible

### Connection Issues

```bash
# Test ClickHouse connection
clickhouse-client --host=localhost --port=9000

# Check network connectivity
telnet your-grpc-server 50051

# Enable debug logging
export LOG_LEVEL=debug
./clustereye-agent-clickhouse
```

### High Memory Usage

Adjust collection settings in `agent.yml`:

```yaml
clickhouse:
  query_monitoring:
    max_queries_per_collection: 50  # Reduce from 100
    max_query_text_length: 2000     # Reduce from 4000
```

## Development

### Project Structure

- `internal/agent/`: Agent orchestration and lifecycle management
- `internal/config/`: Configuration loading and validation
- `internal/logger/`: Multi-level, multi-output logging
- `internal/model/`: Data structures and models
- `internal/collector/`: Data collection logic
  - `collector.go`: Main orchestrator
  - `clickhouse/`: ClickHouse-specific collectors
- `internal/reporter/`: gRPC communication layer

### Building

```bash
# Build
go build -o clustereye-agent-clickhouse main.go

# Build with version info
go build -ldflags "-X main.version=1.0.0 -X main.buildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ) -X main.gitCommit=$(git rev-parse --short HEAD)" -o clustereye-agent-clickhouse main.go

# Run tests
go test ./...

# Format code
go fmt ./...

# Lint
golangci-lint run
```

### Adding New Metrics

1. Update `internal/model/model.go` with new data structures
2. Add collection logic in `internal/collector/clickhouse/`
3. Update the main collector orchestrator if needed

## License

Copyright (c) 2025 CloudNativeWorks

## Support

For issues, questions, or contributions:
- GitHub Issues: https://github.com/CloudNativeWorks/clustereye-agent-clickhouse/issues
- Documentation: https://docs.clustereye.io

## Related Projects

- [clustereye-agent](https://github.com/CloudNativeWorks/clustereye-agent) - Multi-database agent (PostgreSQL, MSSQL, MongoDB)
- [clustereye-server](https://github.com/CloudNativeWorks/clustereye-server) - ClusterEye management server

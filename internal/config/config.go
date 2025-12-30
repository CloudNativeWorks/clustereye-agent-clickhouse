package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v2"
)

// AgentConfig represents the complete configuration for the ClickHouse agent
type AgentConfig struct {
	Key  string `yaml:"key"`
	Name string `yaml:"name"`

	Clickhouse struct {
		Host     string `yaml:"host"`
		Port     string `yaml:"port"`
		User     string `yaml:"user"`
		Pass     string `yaml:"pass"`
		Database string `yaml:"database"`
		Cluster  string `yaml:"cluster"`
		Location string `yaml:"location"`
		Auth     bool   `yaml:"auth"`

		TLSEnabled bool   `yaml:"tls_enabled"`
		CAPath     string `yaml:"ca_path"`

		QueryMonitoring struct {
			Enabled                 bool    `yaml:"enabled"`
			CollectionIntervalSec   int     `yaml:"collection_interval_sec"`
			MaxQueriesPerCollection int     `yaml:"max_queries_per_collection"`
			SlowQueryThresholdMs    int64   `yaml:"slow_query_threshold_ms"`
			CPUThresholdPercent     float64 `yaml:"cpu_threshold_percent"`
			MemoryLimitMB           int64   `yaml:"memory_limit_mb"`
			MaxQueryTextLength      int     `yaml:"max_query_text_length"`
		} `yaml:"query_monitoring"`

		ClusterMonitoring struct {
			Enabled          bool `yaml:"enabled"`
			CheckIntervalSec int  `yaml:"check_interval_sec"`
			ReplicaTimeout   int  `yaml:"replica_timeout"`
		} `yaml:"cluster_monitoring"`
	} `yaml:"clickhouse"`

	GRPC struct {
		ServerAddress          string `yaml:"server_address"`
		TLSEnabled             bool   `yaml:"tls_enabled"`
		InsecureSkipVerify     bool   `yaml:"insecure_skip_verify"`
		ConnectionIntervalSec  int    `yaml:"connection_interval_sec"`
		UnhealthyIntervalSec   int    `yaml:"unhealthy_interval_sec"`
		MaxRetryDelaySec       int    `yaml:"max_retry_delay_sec"`
		MaxRetries             int    `yaml:"max_retries"`
		HealthCheckIntervalSec int    `yaml:"health_check_interval_sec"`
	} `yaml:"grpc"`

	Logging struct {
		Level        string `yaml:"level"`
		UseEventLog  bool   `yaml:"use_event_log"`
		UseFileLog   bool   `yaml:"use_file_log"`
	} `yaml:"logging"`

	ConfigDrift struct {
		Enabled               int      `yaml:"enabled"`
		WatchIntervalSec      int      `yaml:"watch_interval_sec"`
		ClickhouseConfigPaths []string `yaml:"clickhouse_config_paths"`
	} `yaml:"config_drift"`

	Profiling struct {
		Enabled bool `yaml:"enabled"`
		Port    int  `yaml:"port"`
	} `yaml:"profiling"`
}

// LoadConfig loads the configuration from the specified file path
func LoadConfig(configPath string) (*AgentConfig, error) {
	// If no config path provided, try to find it
	if configPath == "" {
		configPath = findConfigPath()
	}

	// Check if config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Generate default config
		if err := generateDefaultConfig(configPath); err != nil {
			return nil, fmt.Errorf("failed to generate default config: %w", err)
		}
		fmt.Printf("Generated default configuration at: %s\n", configPath)
		fmt.Println("Please edit the configuration file and restart the agent.")
		os.Exit(0)
	}

	// Read config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse YAML
	var cfg AgentConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Set defaults
	setDefaults(&cfg)

	// Validate config
	if err := validateConfig(&cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, nil
}

// findConfigPath tries to locate the agent.yml configuration file
func findConfigPath() string {
	// Possible config locations
	paths := []string{
		"agent.yml",
		"./agent.yml",
	}

	if runtime.GOOS == "windows" {
		// Windows-specific paths
		exePath, _ := os.Executable()
		exeDir := filepath.Dir(exePath)
		paths = append(paths,
			filepath.Join(exeDir, "agent.yml"),
			"C:\\Clustereye\\agent.yml",
		)
	} else {
		// Unix-like systems
		paths = append(paths,
			"/etc/clustereye/agent.yml",
			filepath.Join(os.Getenv("HOME"), ".clustereye", "agent.yml"),
		)
	}

	// Find first existing path
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	// Default to agent.yml in current directory
	return "agent.yml"
}

// generateDefaultConfig creates a default configuration file
func generateDefaultConfig(path string) error {
	defaultConfig := `# ClusterEye Agent Configuration for ClickHouse
key: "your-agent-key-here"
name: "clickhouse-agent-1"

clickhouse:
  host: "localhost"
  port: "9000"
  user: "default"
  pass: ""
  database: "default"
  cluster: ""
  location: "default"
  auth: false
  tls_enabled: false
  ca_path: ""

  query_monitoring:
    enabled: true
    collection_interval_sec: 60
    max_queries_per_collection: 100
    slow_query_threshold_ms: 1000
    cpu_threshold_percent: 80.0
    memory_limit_mb: 1024
    max_query_text_length: 4000

  cluster_monitoring:
    enabled: true
    check_interval_sec: 30
    replica_timeout: 10

grpc:
  server_address: "localhost:50051"
  tls_enabled: false
  insecure_skip_verify: false
  connection_interval_sec: 5
  unhealthy_interval_sec: 30
  max_retry_delay_sec: 300
  max_retries: 5
  health_check_interval_sec: 10

logging:
  level: "info"
  use_event_log: false
  use_file_log: true

config_drift:
  enabled: 1
  watch_interval_sec: 30
  clickhouse_config_paths:
    - "/etc/clickhouse-server/config.xml"
    - "/etc/clickhouse-server/users.xml"

profiling:
  enabled: false
  port: 6060
`

	// Create directory if needed
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Write default config
	return os.WriteFile(path, []byte(defaultConfig), 0644)
}

// setDefaults sets default values for unspecified config options
func setDefaults(cfg *AgentConfig) {
	// ClickHouse defaults
	if cfg.Clickhouse.Port == "" {
		cfg.Clickhouse.Port = "9000"
	}
	if cfg.Clickhouse.Host == "" {
		cfg.Clickhouse.Host = "localhost"
	}
	if cfg.Clickhouse.User == "" {
		cfg.Clickhouse.User = "default"
	}
	if cfg.Clickhouse.Database == "" {
		cfg.Clickhouse.Database = "default"
	}

	// Query monitoring defaults
	if cfg.Clickhouse.QueryMonitoring.CollectionIntervalSec == 0 {
		cfg.Clickhouse.QueryMonitoring.CollectionIntervalSec = 60
	}
	if cfg.Clickhouse.QueryMonitoring.MaxQueriesPerCollection == 0 {
		cfg.Clickhouse.QueryMonitoring.MaxQueriesPerCollection = 100
	}
	if cfg.Clickhouse.QueryMonitoring.MaxQueryTextLength == 0 {
		cfg.Clickhouse.QueryMonitoring.MaxQueryTextLength = 4000
	}

	// Cluster monitoring defaults
	if cfg.Clickhouse.ClusterMonitoring.CheckIntervalSec == 0 {
		cfg.Clickhouse.ClusterMonitoring.CheckIntervalSec = 30
	}
	if cfg.Clickhouse.ClusterMonitoring.ReplicaTimeout == 0 {
		cfg.Clickhouse.ClusterMonitoring.ReplicaTimeout = 10
	}

	// gRPC defaults
	if cfg.GRPC.ConnectionIntervalSec == 0 {
		cfg.GRPC.ConnectionIntervalSec = 5
	}
	if cfg.GRPC.UnhealthyIntervalSec == 0 {
		cfg.GRPC.UnhealthyIntervalSec = 30
	}
	if cfg.GRPC.MaxRetryDelaySec == 0 {
		cfg.GRPC.MaxRetryDelaySec = 300
	}
	if cfg.GRPC.MaxRetries == 0 {
		cfg.GRPC.MaxRetries = 5
	}
	if cfg.GRPC.HealthCheckIntervalSec == 0 {
		cfg.GRPC.HealthCheckIntervalSec = 10
	}

	// Logging defaults
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}

	// Config drift defaults
	if cfg.ConfigDrift.WatchIntervalSec == 0 {
		cfg.ConfigDrift.WatchIntervalSec = 30
	}

	// Profiling defaults
	if cfg.Profiling.Port == 0 {
		cfg.Profiling.Port = 6060
	}
}

// validateConfig validates the configuration
func validateConfig(cfg *AgentConfig) error {
	if cfg.Key == "" || cfg.Key == "your-agent-key-here" {
		return fmt.Errorf("agent key is required")
	}

	if cfg.Name == "" {
		return fmt.Errorf("agent name is required")
	}

	if cfg.Clickhouse.Host == "" {
		return fmt.Errorf("clickhouse host is required")
	}

	if cfg.GRPC.ServerAddress == "" {
		return fmt.Errorf("grpc server address is required")
	}

	return nil
}

// GetLogLevel returns the configured log level or from environment variable
func (cfg *AgentConfig) GetLogLevel() string {
	if level := os.Getenv("LOG_LEVEL"); level != "" {
		return level
	}
	return cfg.Logging.Level
}

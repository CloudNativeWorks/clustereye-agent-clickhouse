package reporter

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/CloudNativeWorks/clustereye-agent-clickhouse/internal/config"
	"github.com/CloudNativeWorks/clustereye-agent-clickhouse/internal/logger"
	"github.com/CloudNativeWorks/clustereye-agent-clickhouse/internal/model"
	pb "github.com/CloudNativeWorks/clustereye-api/pkg/agent"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// Reporter handles gRPC communication with the ClusterEye server
type Reporter struct {
	cfg         *config.AgentConfig
	conn        *grpc.ClientConn
	client      pb.AgentServiceClient
	stream      pb.AgentService_ConnectClient
	connMu      sync.Mutex
	streamMu    sync.Mutex
	isConnected bool
	agentID     string
}

var (
	globalReporter *Reporter
	reporterMutex  sync.RWMutex
)

// NewReporter creates a new reporter instance
func NewReporter(cfg *config.AgentConfig) *Reporter {
	return &Reporter{
		cfg:         cfg,
		isConnected: false,
	}
}

// GetGlobalReporter returns the global reporter instance
func GetGlobalReporter() *Reporter {
	reporterMutex.RLock()
	defer reporterMutex.RUnlock()
	return globalReporter
}

// SetGlobalReporter sets the global reporter instance
func SetGlobalReporter(reporter *Reporter) {
	reporterMutex.Lock()
	defer reporterMutex.Unlock()
	globalReporter = reporter
}

// Connect establishes connection to the gRPC server
func (r *Reporter) Connect() error {
	r.connMu.Lock()
	defer r.connMu.Unlock()

	if r.conn != nil {
		return nil
	}

	logger.Info("Connecting to gRPC server at %s", r.cfg.GRPC.ServerAddress)

	// Setup keepalive parameters
	kacp := keepalive.ClientParameters{
		Time:                20 * time.Second,
		Timeout:             10 * time.Second,
		PermitWithoutStream: true,
	}

	// Setup dial options
	opts := []grpc.DialOption{
		grpc.WithKeepaliveParams(kacp),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(128 * 1024 * 1024), // 128MB
			grpc.MaxCallSendMsgSize(128 * 1024 * 1024), // 128MB
		),
	}

	// Add TLS or insecure credentials
	if r.cfg.GRPC.TLSEnabled {
		// TODO: Add TLS credentials
		logger.Warning("TLS is enabled but not implemented yet, using insecure connection")
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	// Dial server
	conn, err := grpc.Dial(r.cfg.GRPC.ServerAddress, opts...)
	if err != nil {
		return fmt.Errorf("failed to connect to gRPC server: %w", err)
	}

	r.conn = conn
	r.client = pb.NewAgentServiceClient(conn)
	r.isConnected = true
	logger.Info("Successfully connected to gRPC server")

	return nil
}

// Disconnect closes the connection to the gRPC server
func (r *Reporter) Disconnect() {
	r.connMu.Lock()
	defer r.connMu.Unlock()

	if r.stream != nil {
		r.stream.CloseSend()
		r.stream = nil
	}

	if r.conn != nil {
		r.conn.Close()
		r.conn = nil
		r.isConnected = false
		logger.Info("Disconnected from gRPC server")
	}
}

// IsConnected returns the connection status
func (r *Reporter) IsConnected() bool {
	r.connMu.Lock()
	defer r.connMu.Unlock()
	return r.isConnected
}

// getLocalIP returns the local IP address
func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "unknown"
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}

	return "unknown"
}

// AgentRegistration registers the agent with the server
func (r *Reporter) AgentRegistration(testResult, platform string) error {
	if !r.IsConnected() {
		return fmt.Errorf("not connected to server")
	}

	logger.Info("Registering agent with server...")

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	agentInfo := &pb.AgentInfo{
		Key:      r.cfg.Key,
		Hostname: hostname,
		Ip:       getLocalIP(),
		Platform: platform, // "clickhouse"
		Auth:     r.cfg.Clickhouse.Auth,
		Test:     testResult,
	}

	req := &pb.RegisterRequest{
		AgentInfo: agentInfo,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := r.client.Register(ctx, req)
	if err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}

	if resp.Registration != nil {
		logger.Info("Agent registration status: %s - %s",
			resp.Registration.Status, resp.Registration.Message)

		// Store agent ID if provided
		if agentInfo.AgentId != "" {
			r.agentID = agentInfo.AgentId
		}
	}

	return nil
}

// SendData sends collected data to the server
func (r *Reporter) SendData(data interface{}) error {
	if !r.IsConnected() {
		return fmt.Errorf("not connected to server")
	}

	systemData, ok := data.(*model.SystemData)
	if !ok {
		return fmt.Errorf("invalid data type, expected *model.SystemData")
	}

	// Send system metrics
	if systemData.System != nil {
		if err := r.sendSystemMetrics(systemData.System); err != nil {
			logger.Warning("Failed to send system metrics: %v", err)
		}
	}

	// Send ClickHouse info as metrics
	if systemData.Clickhouse != nil && systemData.Clickhouse.Info != nil {
		if err := r.sendClickhouseInfo(systemData.Clickhouse.Info); err != nil {
			logger.Warning("Failed to send ClickHouse info: %v", err)
		}
	}

	// Send ClickHouse metrics
	if systemData.Clickhouse != nil && systemData.Clickhouse.Metrics != nil {
		if err := r.sendClickhouseMetrics(systemData.Clickhouse.Metrics); err != nil {
			logger.Warning("Failed to send ClickHouse metrics: %v", err)
		}
	}

	logger.Debug("Data sent successfully")
	return nil
}

// sendSystemMetrics sends system-level metrics to the server
func (r *Reporter) sendSystemMetrics(metrics *model.SystemMetrics) error {
	pbMetrics := ConvertSystemMetrics(metrics)

	req := &pb.SystemMetricsRequest{
		AgentId: r.cfg.Key,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := r.client.SendSystemMetrics(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to send system metrics: %w", err)
	}

	// Update response with metrics
	resp.Metrics = pbMetrics

	logger.Debug("System metrics sent: %s", resp.Status)
	return nil
}

// sendClickhouseInfo sends ClickHouse instance information
func (r *Reporter) sendClickhouseInfo(info *model.ClickhouseInfo) error {
	// Convert model to protobuf
	pbInfo := &pb.ClickhouseInfo{
		ClusterName:  info.ClusterName,
		Ip:           info.IP,
		Hostname:     info.Hostname,
		NodeStatus:   info.NodeStatus,
		Version:      info.Version,
		Location:     info.Location,
		Status:       info.Status,
		Port:         info.Port,
		Uptime:       info.Uptime,
		ConfigPath:   info.ConfigPath,
		DataPath:     info.DataPath,
		IsReplicated: info.IsReplicated,
		ReplicaCount: int32(info.ReplicaCount),
		ShardCount:   int32(info.ShardCount),
		TotalVcpu:    int32(info.TotalVCPU),
		TotalMemory:  info.TotalMemory,
	}

	req := &pb.ClickhouseInfoRequest{
		ClickhouseInfo: pbInfo,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := r.client.SendClickhouseInfo(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to send ClickHouse info: %w", err)
	}

	logger.Debug("ClickHouse info sent: %s", resp.Status)
	return nil
}

// sendClickhouseMetrics sends ClickHouse performance metrics
func (r *Reporter) sendClickhouseMetrics(metrics *model.ClickhouseMetrics) error {
	batch := ConvertClickhouseMetricsToMetrics(r.cfg.Key, metrics)

	req := &pb.SendMetricsRequest{
		Batch: batch,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := r.client.SendMetrics(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to send ClickHouse metrics: %w", err)
	}

	logger.Debug("ClickHouse metrics sent: %s (%d metrics)", resp.Status, resp.ProcessedCount)
	return nil
}

// StartStream starts the bidirectional stream with the server
func (r *Reporter) StartStream(ctx context.Context) error {
	if !r.IsConnected() {
		if err := r.Connect(); err != nil {
			return err
		}
	}

	logger.Info("Starting bidirectional stream...")

	r.streamMu.Lock()
	stream, err := r.client.Connect(ctx)
	if err != nil {
		r.streamMu.Unlock()
		return fmt.Errorf("failed to create stream: %w", err)
	}
	r.stream = stream
	r.streamMu.Unlock()

	// Send initial agent info
	hostname, _ := os.Hostname()
	agentInfo := &pb.AgentInfo{
		Key:      r.cfg.Key,
		Hostname: hostname,
		Ip:       getLocalIP(),
		Platform: "clickhouse",
		Auth:     r.cfg.Clickhouse.Auth,
	}

	agentMsg := &pb.AgentMessage{
		Payload: &pb.AgentMessage_AgentInfo{
			AgentInfo: agentInfo,
		},
	}

	if err := stream.Send(agentMsg); err != nil {
		return fmt.Errorf("failed to send agent info: %w", err)
	}

	logger.Info("Agent info sent to server via stream")

	// Start goroutine to receive server messages
	go r.handleServerMessages(ctx, stream)

	// Keep stream alive
	<-ctx.Done()
	logger.Info("Stream context done, closing stream")

	return nil
}

// handleServerMessages handles incoming messages from the server
func (r *Reporter) handleServerMessages(ctx context.Context, stream pb.AgentService_ConnectClient) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			serverMsg, err := stream.Recv()
			if err != nil {
				logger.Warning("Error receiving server message: %v", err)
				return
			}

			r.processServerMessage(serverMsg)
		}
	}
}

// processServerMessage processes messages received from the server
func (r *Reporter) processServerMessage(msg *pb.ServerMessage) {
	if msg == nil {
		return
	}

	switch payload := msg.Payload.(type) {
	case *pb.ServerMessage_Query:
		logger.Info("Received query from server: %s", payload.Query.Command)
		// TODO: Execute query and send response

	case *pb.ServerMessage_Error:
		logger.Error("Server error: %s - %s", payload.Error.Code, payload.Error.Message)

	case *pb.ServerMessage_Registration:
		logger.Info("Registration result: %s - %s",
			payload.Registration.Status, payload.Registration.Message)

	case *pb.ServerMessage_MetricsRequest:
		logger.Debug("Metrics request from server for agent: %s", payload.MetricsRequest.AgentId)
		// TODO: Collect and send requested metrics

	default:
		logger.Debug("Received unknown message type from server")
	}
}

// SendHeartbeat sends a heartbeat to the server
func (r *Reporter) SendHeartbeat() error {
	r.streamMu.Lock()
	stream := r.stream
	r.streamMu.Unlock()

	if stream == nil {
		return fmt.Errorf("stream not established")
	}

	heartbeat := &pb.Heartbeat{
		Timestamp: time.Now().Unix(),
	}

	msg := &pb.AgentMessage{
		Payload: &pb.AgentMessage_Heartbeat{
			Heartbeat: heartbeat,
		},
	}

	if err := stream.Send(msg); err != nil {
		return fmt.Errorf("failed to send heartbeat: %w", err)
	}

	logger.Debug("Heartbeat sent")
	return nil
}

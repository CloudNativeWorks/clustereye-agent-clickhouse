package reporter

import (
	"context"
	"crypto/tls"
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
	"google.golang.org/grpc/credentials"
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

	// Setup transport credentials
	var creds credentials.TransportCredentials
	if r.cfg.GRPC.TLSEnabled {
		if r.cfg.GRPC.InsecureSkipVerify {
			// TLS with certificate verification disabled (for development)
			creds = credentials.NewTLS(&tls.Config{
				InsecureSkipVerify: true,
				NextProtos:         []string{"h2"}, // Force HTTP/2
			})
			logger.Info("Using TLS with certificate verification disabled")
		} else {
			// Secure TLS connection (for production)
			creds = credentials.NewTLS(&tls.Config{
				NextProtos: []string{"h2"}, // Force HTTP/2
			})
			logger.Info("Using secure TLS connection")
		}
	} else {
		// No TLS
		creds = insecure.NewCredentials()
		logger.Info("Using insecure connection (no TLS)")
	}

	// Setup dial options
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
		grpc.WithKeepaliveParams(kacp),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(128 * 1024 * 1024), // 128MB
			grpc.MaxCallSendMsgSize(128 * 1024 * 1024), // 128MB
		),
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

// GetClient returns the gRPC client
func (r *Reporter) GetClient() pb.AgentServiceClient {
	r.connMu.Lock()
	defer r.connMu.Unlock()
	return r.client
}

// RefreshClient reconnects and returns a new client
func (r *Reporter) RefreshClient() (pb.AgentServiceClient, error) {
	r.connMu.Lock()
	defer r.connMu.Unlock()

	// Close existing connection
	if r.conn != nil {
		r.conn.Close()
		r.conn = nil
		r.client = nil
		r.isConnected = false
	}

	// Reconnect
	logger.Info("Refreshing gRPC client connection...")

	// Setup keepalive parameters
	kacp := keepalive.ClientParameters{
		Time:                20 * time.Second,
		Timeout:             10 * time.Second,
		PermitWithoutStream: true,
	}

	// Setup transport credentials
	var creds credentials.TransportCredentials
	if r.cfg.GRPC.TLSEnabled {
		if r.cfg.GRPC.InsecureSkipVerify {
			creds = credentials.NewTLS(&tls.Config{
				InsecureSkipVerify: true,
				NextProtos:         []string{"h2"},
			})
		} else {
			creds = credentials.NewTLS(&tls.Config{
				NextProtos: []string{"h2"},
			})
		}
	} else {
		creds = insecure.NewCredentials()
	}

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
		grpc.WithKeepaliveParams(kacp),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(128 * 1024 * 1024),
			grpc.MaxCallSendMsgSize(128 * 1024 * 1024),
		),
	}

	conn, err := grpc.Dial(r.cfg.GRPC.ServerAddress, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to reconnect to gRPC server: %w", err)
	}

	r.conn = conn
	r.client = pb.NewAgentServiceClient(conn)
	r.isConnected = true
	logger.Info("gRPC client refreshed successfully")

	return r.client, nil
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

	// Send ClickHouse data with advanced metrics using new API
	if systemData.Clickhouse != nil {
		if err := r.sendClickhouseData(systemData.Clickhouse); err != nil {
			logger.Warning("Failed to send ClickHouse data: %v", err)
			// Fallback to old methods for compatibility
			if systemData.Clickhouse.Info != nil {
				if err := r.sendClickhouseInfo(systemData.Clickhouse.Info); err != nil {
					logger.Warning("Failed to send ClickHouse info: %v", err)
				}
			}
			if systemData.Clickhouse.Metrics != nil {
				if err := r.sendClickhouseMetrics(systemData.Clickhouse.Metrics); err != nil {
					logger.Warning("Failed to send ClickHouse metrics: %v", err)
				}
			}
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
	// Get hostname for the request
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	// Convert model metrics to protobuf
	pbMetrics := &pb.ClickhouseMetrics{
		CurrentConnections:       metrics.CurrentConnections,
		MaxConnections:           metrics.MaxConnections,
		QueriesPerSecond:         metrics.QueriesPerSecond,
		SelectQueriesPerSecond:   metrics.SelectQueriesPerSecond,
		InsertQueriesPerSecond:   metrics.InsertQueriesPerSecond,
		RunningQueries:           metrics.RunningQueries,
		QueuedQueries:            metrics.QueuedQueries,
		MemoryUsage:              metrics.MemoryUsage,
		MemoryAvailable:          metrics.MemoryAvailable,
		MemoryPercent:            metrics.MemoryPercent,
		DiskUsage:                metrics.DiskUsage,
		DiskAvailable:            metrics.DiskAvailable,
		DiskPercent:              metrics.DiskPercent,
		MergesInProgress:         metrics.MergesInProgress,
		PartsCount:               metrics.PartsCount,
		RowsRead:                 metrics.RowsRead,
		BytesRead:                metrics.BytesRead,
		NetworkReceiveBytes:      metrics.NetworkReceiveBytes,
		NetworkSendBytes:         metrics.NetworkSendBytes,
		CpuUsage:                 metrics.CPUUsage,
		MarkCacheBytes:           metrics.MarkCacheBytes,
		MarkCacheFiles:           metrics.MarkCacheFiles,
		CollectionTime:           metrics.CollectionTime.UnixNano(),
	}

	req := &pb.ClickhouseMetricsRequest{
		AgentKey:    r.cfg.Key,
		ClusterName: r.cfg.Clickhouse.Cluster,
		Hostname:    hostname,
		Metrics:     pbMetrics,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := r.client.SendClickhouseMetrics(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to send ClickHouse metrics: %w", err)
	}

	logger.Debug("ClickHouse metrics sent: %s - %s", resp.Status, resp.Message)
	return nil
}

// sendClickhouseData sends complete ClickHouse data including advanced metrics
func (r *Reporter) sendClickhouseData(chData *model.ClickhouseData) error {
	if chData == nil {
		return fmt.Errorf("no clickhouse data provided")
	}

	// Build the protobuf request
	pbData := &pb.ClickhouseData{}

	// Convert Info
	if chData.Info != nil {
		pbData.Info = &pb.ClickhouseInfo{
			ClusterName:  chData.Info.ClusterName,
			Ip:           chData.Info.IP,
			Hostname:     chData.Info.Hostname,
			NodeStatus:   chData.Info.NodeStatus,
			Version:      chData.Info.Version,
			Location:     chData.Info.Location,
			Status:       chData.Info.Status,
			Port:         chData.Info.Port,
			Uptime:       chData.Info.Uptime,
			ConfigPath:   chData.Info.ConfigPath,
			DataPath:     chData.Info.DataPath,
			IsReplicated: chData.Info.IsReplicated,
			ReplicaCount: int32(chData.Info.ReplicaCount),
			ShardCount:   int32(chData.Info.ShardCount),
			TotalVcpu:    int32(chData.Info.TotalVCPU),
			TotalMemory:  chData.Info.TotalMemory,
		}
	}

	// Convert Metrics
	if chData.Metrics != nil {
		pbData.Metrics = &pb.ClickhouseMetrics{
			CurrentConnections:     chData.Metrics.CurrentConnections,
			MaxConnections:         chData.Metrics.MaxConnections,
			QueriesPerSecond:       chData.Metrics.QueriesPerSecond,
			SelectQueriesPerSecond: chData.Metrics.SelectQueriesPerSecond,
			InsertQueriesPerSecond: chData.Metrics.InsertQueriesPerSecond,
			RunningQueries:         chData.Metrics.RunningQueries,
			QueuedQueries:          chData.Metrics.QueuedQueries,
			MemoryUsage:            chData.Metrics.MemoryUsage,
			MemoryAvailable:        chData.Metrics.MemoryAvailable,
			MemoryPercent:          chData.Metrics.MemoryPercent,
			DiskUsage:              chData.Metrics.DiskUsage,
			DiskAvailable:          chData.Metrics.DiskAvailable,
			DiskPercent:            chData.Metrics.DiskPercent,
			MergesInProgress:       chData.Metrics.MergesInProgress,
			PartsCount:             chData.Metrics.PartsCount,
			RowsRead:               chData.Metrics.RowsRead,
			BytesRead:              chData.Metrics.BytesRead,
			NetworkReceiveBytes:    chData.Metrics.NetworkReceiveBytes,
			NetworkSendBytes:       chData.Metrics.NetworkSendBytes,
			CpuUsage:               chData.Metrics.CPUUsage,
			MarkCacheBytes:         chData.Metrics.MarkCacheBytes,
			MarkCacheFiles:         chData.Metrics.MarkCacheFiles,
			CollectionTime:         chData.Metrics.CollectionTime.UnixNano(),
		}
	}

	// Convert MergeMetrics
	if chData.MergeMetrics != nil {
		pbMerge := &pb.MergeMetrics{
			ActiveMerges:        chData.MergeMetrics.ActiveMerges,
			MutationMerges:      chData.MergeMetrics.MutationMerges,
			BackgroundMerges:    chData.MergeMetrics.BackgroundMerges,
			TotalMergeTimeSec:   chData.MergeMetrics.TotalMergeTime,
			MaxMergeElapsedSec:  chData.MergeMetrics.MaxMergeElapsed,
			AvgMergeElapsedSec:  chData.MergeMetrics.AvgMergeElapsed,
			MergeBytesRead:      chData.MergeMetrics.MergeBytesRead,
			MergeBytesWritten:   chData.MergeMetrics.MergeBytesWritten,
			MergeRowsRead:       chData.MergeMetrics.MergeRowsRead,
			MergeRowsWritten:    chData.MergeMetrics.MergeRowsWritten,
			MergeThroughputMbS:  chData.MergeMetrics.MergeThroughputMBs,
			MergeBacklog:        chData.MergeMetrics.MergeBacklog,
			MergeQueueSize:      chData.MergeMetrics.MergeQueueSize,
			CollectionTime:      chData.MergeMetrics.CollectionTime.UnixNano(),
		}

		// Convert table merges
		for _, tm := range chData.MergeMetrics.TableMerges {
			pbMerge.TableMerges = append(pbMerge.TableMerges, &pb.TableMergeInfo{
				Database:       tm.Database,
				Table:          tm.Table,
				ActiveMerges:   tm.ActiveMerges,
				MergeProgress:  tm.MergeProgress,
				BytesProcessed: tm.BytesProcessed,
				ElapsedTimeSec: tm.ElapsedTime,
				IsMutation:     tm.IsMutation,
			})
		}

		pbData.MergeMetrics = pbMerge
	}

	// Convert PartsMetrics
	if chData.PartsMetrics != nil {
		pbParts := &pb.PartsMetrics{
			TotalParts:        chData.PartsMetrics.TotalParts,
			ActiveParts:       chData.PartsMetrics.ActiveParts,
			InactiveParts:     chData.PartsMetrics.InactiveParts,
			SmallPartsCount:   chData.PartsMetrics.SmallPartsCount,
			SmallPartsRatio:   chData.PartsMetrics.SmallPartsRatio,
			TotalBytesOnDisk:  chData.PartsMetrics.TotalBytesOnDisk,
			TotalRows:         chData.PartsMetrics.TotalRows,
			MaxPartsPerTable:  chData.PartsMetrics.MaxPartsPerTable,
			CollectionTime:    chData.PartsMetrics.CollectionTime.UnixNano(),
		}

		// Convert tables with too many parts
		for _, t := range chData.PartsMetrics.TablesWithTooManyParts {
			pbParts.TablesWithTooManyParts = append(pbParts.TablesWithTooManyParts, &pb.TablePartsInfo{
				Database:    t.Database,
				Table:       t.Table,
				PartsCount:  t.PartsCount,
				ActiveParts: t.ActiveParts,
				SmallParts:  t.SmallParts,
				TotalBytes:  t.TotalBytes,
				TotalRows:   t.TotalRows,
				AvgPartSize: t.AvgPartSize,
				Engine:      t.Engine,
			})
		}

		pbData.PartsMetrics = pbParts
	}

	// Convert MemoryPressure
	if chData.MemoryPressure != nil {
		pbData.MemoryPressure = &pb.MemoryPressureMetrics{
			MemoryTracking:                   chData.MemoryPressure.MemoryTracking,
			MemoryTrackingForMerges:          chData.MemoryPressure.MemoryTrackingForMerges,
			MemoryTrackingForQueries:         chData.MemoryPressure.MemoryTrackingForQueries,
			MaxServerMemoryUsage:             chData.MemoryPressure.MaxServerMemoryUsage,
			MemoryUsagePercent:               chData.MemoryPressure.MemoryUsagePercent,
			QueryMemoryLimitExceeded:         chData.MemoryPressure.QueryMemoryLimitExceeded,
			MemoryOvercommitWaitTimeMicrosec: chData.MemoryPressure.MemoryOvercommitWaitTime,
			CannotAllocateMemory:             chData.MemoryPressure.CannotAllocateMemory,
			MemoryAllocateFail:               chData.MemoryPressure.MemoryAllocateFail,
			MarkCacheBytes:                   chData.MemoryPressure.MarkCacheBytes,
			UncompressedCacheBytes:           chData.MemoryPressure.UncompressedCacheBytes,
			CollectionTime:                   chData.MemoryPressure.CollectionTime.UnixNano(),
		}
	}

	// Convert QueryConcurrency
	if chData.QueryConcurrency != nil {
		pbQuery := &pb.QueryConcurrencyMetrics{
			RunningQueries:          chData.QueryConcurrency.RunningQueries,
			RunningSelectQueries:    chData.QueryConcurrency.RunningSelectQueries,
			RunningInsertQueries:    chData.QueryConcurrency.RunningInsertQueries,
			QueuedQueries:           chData.QueryConcurrency.QueuedQueries,
			QueryPreempted:          chData.QueryConcurrency.QueryPreempted,
			LongRunningQueries:      chData.QueryConcurrency.LongRunningQueries,
			VeryLongRunningQueries:  chData.QueryConcurrency.VeryLongRunningQueries,
			MaxQueryElapsedSec:      chData.QueryConcurrency.MaxQueryElapsed,
			TotalQueryMemoryUsage:   chData.QueryConcurrency.TotalQueryMemoryUsage,
			TotalReadRows:           chData.QueryConcurrency.TotalReadRows,
			TotalReadBytes:          chData.QueryConcurrency.TotalReadBytes,
			CollectionTime:          chData.QueryConcurrency.CollectionTime.UnixNano(),
		}

		// Convert long running query details
		for _, q := range chData.QueryConcurrency.LongRunningQueryDetails {
			pbQuery.LongRunningQueryDetails = append(pbQuery.LongRunningQueryDetails, &pb.LongRunningQueryInfo{
				QueryId:        q.QueryID,
				User:           q.User,
				Query:          q.Query,
				ElapsedTimeSec: q.ElapsedTime,
				MemoryUsage:    q.MemoryUsage,
				ReadRows:       q.ReadRows,
				ReadBytes:      q.ReadBytes,
				Database:       q.Database,
				IsCancelled:    q.IsCancelled,
			})
		}

		pbData.QueryConcurrency = pbQuery
	}

	// Convert Errors
	for _, e := range chData.Errors {
		pbData.Errors = append(pbData.Errors, &pb.ClickHouseError{
			Name:             e.Name,
			Code:             e.Code,
			Value:            e.Value,
			LastErrorTime:    e.LastErrorTime.Unix(),
			LastErrorMessage: e.LastErrorMessage,
			RemoteHosts:      e.RemoteHosts,
		})
	}

	req := &pb.ClickhouseDataRequest{
		AgentKey: r.cfg.Key,
		Data:     pbData,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := r.client.SendClickhouseData(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to send ClickHouse data: %w", err)
	}

	logger.Debug("ClickHouse data sent: %s - %s", resp.Status, resp.Message)
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

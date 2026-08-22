package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/tyagiquamar/relaydb/gen"
	"github.com/tyagiquamar/relaydb/internal/config"
	"github.com/tyagiquamar/relaydb/internal/consumer"
	"github.com/tyagiquamar/relaydb/internal/telemetry"
)

// Server is the gRPC server.
type Server struct {
	relaydbv1.UnimplementedConsumerServiceServer
	cfg      config.Config
	consumer *consumer.Service
	logger   *slog.Logger
}

// NewServer creates a gRPC server.
func NewServer(cfg config.Config, consumerSvc *consumer.Service) *Server {
	return &Server{
		cfg:      cfg,
		consumer: consumerSvc,
		logger:   telemetry.With("service", "grpc"),
	}
}

// Serve starts the gRPC server.
func (s *Server) Serve(ctx context.Context, addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			AuthInterceptor(s.cfg.AdminAPIKeyID, s.cfg.AdminAPIKey, s.cfg.ReaderAPIKeyID, s.cfg.ReaderAPIKey),
		),
	)

	relaydbv1.RegisterConsumerServiceServer(srv, s)

	s.logger.Info("grpc listening", "addr", addr)

	go func() {
		<-ctx.Done()
		srv.GracefulStop()
	}()

	return srv.Serve(lis)
}

// Poll implements ConsumerService.Poll.
func (s *Server) Poll(ctx context.Context, req *relaydbv1.PollRequest) (*relaydbv1.PollResponse, error) {
	events, lease, err := s.consumer.Poll(ctx, req.GroupId, req.MemberId,
		int(req.MaxEvents), time.Duration(req.MaxWaitMs)*time.Millisecond)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "poll: %v", err)
	}

	pbEvents := make([]*relaydbv1.Event, len(events))
	for i, e := range events {
		pbEvents[i] = &relaydbv1.Event{
			Id:             e.ID[:],
			SourceId:       e.SourceID,
			TransactionId:  e.TransactionID,
			CommitEndLsn:   e.CommitEndLSN,
			SequenceNumber: int32(e.SequenceNumber),
			SchemaName:     e.SchemaName,
			TableName:      e.TableName,
			Operation:      string(e.Operation),
			PayloadHash:    e.PayloadHash,
			CreatedAtUnix:  e.CreatedAt.Unix(),
		}
	}

	return &relaydbv1.PollResponse{
		Events:          pbEvents,
		LeaseOwner:      lease.Owner,
		LeaseGeneration: lease.Generation,
		Partition:       int32(lease.Partition),
	}, nil
}

// Ack implements ConsumerService.Ack.
func (s *Server) Ack(ctx context.Context, req *relaydbv1.AckRequest) (*relaydbv1.AckResponse, error) {
	err := s.consumer.Ack(ctx, req.GroupId, int(req.Partition),
		req.LeaseOwner, req.LeaseGeneration,
		req.CommitEndLsn, int(req.SequenceNumber), req.LastEventId)
	if err != nil {
		return &relaydbv1.AckResponse{Success: false, Error: err.Error()}, nil
	}
	return &relaydbv1.AckResponse{Success: true}, nil
}

// Nack implements ConsumerService.Nack.
func (s *Server) Nack(ctx context.Context, req *relaydbv1.NackRequest) (*relaydbv1.NackResponse, error) {
	err := s.consumer.Nack(ctx, req.GroupId, int(req.Partition),
		req.LeaseOwner, req.LeaseGeneration,
		req.EventIds, time.Duration(req.RetryAfterMs)*time.Millisecond)
	if err != nil {
		return &relaydbv1.NackResponse{Success: false, Error: err.Error()}, nil
	}
	return &relaydbv1.NackResponse{Success: true}, nil
}

// Heartbeat implements ConsumerService.Heartbeat.
func (s *Server) Heartbeat(ctx context.Context, req *relaydbv1.HeartbeatRequest) (*relaydbv1.HeartbeatResponse, error) {
	leaseObj, err := s.consumer.Heartbeat(ctx, req.GroupId, int(req.Partition), req.LeaseOwner,
		req.LeaseGeneration, s.cfg.LeaseDuration)
	if err != nil {
		return &relaydbv1.HeartbeatResponse{Success: false, LeaseGeneration: req.LeaseGeneration}, nil
	}
	return &relaydbv1.HeartbeatResponse{
		Success:         true,
		LeaseGeneration: leaseObj.Generation,
		ExpiresAtUnix:   leaseObj.ExpiresAt.Unix(),
	}, nil
}

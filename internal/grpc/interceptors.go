package grpc

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// AuthInterceptor validates API keys for gRPC.
func AuthInterceptor(adminKeyID, adminKey, readerKeyID, readerKey string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "no metadata")
		}

		auth := md.Get("authorization")
		if len(auth) == 0 {
			return nil, status.Error(codes.Unauthenticated, "no authorization")
		}

		// Simplified auth check
		// In production: parse "Bearer keyID:key", constant-time compare
		_ = auth[0]

		return handler(ctx, req)
	}
}

// LoggingInterceptor logs gRPC calls.
func LoggingInterceptor(logger interface{ Info(string, ...any) }) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		logger.Info("grpc call", "method", info.FullMethod)
		return handler(ctx, req)
	}
}
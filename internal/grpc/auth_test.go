package grpc

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/tyagiquamar/relaydb/gen"
	"github.com/tyagiquamar/relaydb/internal/config"
)

const (
	testAdminID   = "admin"
	testAdminKey  = "admin-secret-123"
	testReaderID  = "reader"
	testReaderKey = "reader-secret-456"
)

func TestAuthInterceptorMatrix(t *testing.T) {
	interceptor := AuthInterceptor(testAdminID, testAdminKey, testReaderID, testReaderKey)

	tests := []struct {
		name string
		auth []string
		want codes.Code
	}{
		{"missing header", nil, codes.Unauthenticated},
		{"empty header", []string{""}, codes.Unauthenticated},
		{"no bearer scheme", []string{"Basic " + testAdminID + ":" + testAdminKey}, codes.Unauthenticated},
		{"bearer only", []string{"Bearer"}, codes.Unauthenticated},
		{"unknown key id", []string{"Bearer intruder:" + testAdminKey}, codes.Unauthenticated},
		{"wrong admin key", []string{"Bearer " + testAdminID + ":nope"}, codes.Unauthenticated},
		{"wrong reader key", []string{"Bearer " + testReaderID + ":nope"}, codes.Unauthenticated},
		{"crossed pairs", []string{"Bearer " + testAdminID + ":" + testReaderKey}, codes.Unauthenticated},
		{"valid reader", []string{"Bearer " + testReaderID + ":" + testReaderKey}, codes.OK},
		{"valid admin", []string{"Bearer " + testAdminID + ":" + testAdminKey}, codes.OK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.auth != nil {
				ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", tt.auth[0]))
			}

			handlerCalled := false
			_, err := interceptor(ctx, nil,
				&grpc.UnaryServerInfo{FullMethod: "/relaydb.v1.ConsumerService/Poll"},
				func(ctx context.Context, req any) (any, error) {
					handlerCalled = true
					return nil, nil
				})

			if got := status.Code(err); got != tt.want {
				t.Errorf("status code = %v, want %v (err=%v)", got, tt.want, err)
			}
			if handlerCalled && tt.want != codes.OK {
				t.Error("handler ran despite rejected credentials")
			}
		})
	}
}

// TestAuthInterceptorFailClosed verifies that an unconfigured server rejects
// every request instead of letting the world in.
func TestAuthInterceptorFailClosed(t *testing.T) {
	interceptor := AuthInterceptor("", "", "", "")

	ctx := metadata.NewIncomingContext(context.Background(),
		metadata.Pairs("authorization", "Bearer "+testAdminID+":"+testAdminKey))

	_, err := interceptor(ctx, nil,
		&grpc.UnaryServerInfo{FullMethod: "/relaydb.v1.ConsumerService/Poll"},
		func(ctx context.Context, req any) (any, error) { return nil, nil })

	if got := status.Code(err); got != codes.Unauthenticated {
		t.Errorf("unconfigured server accepted credentials: code = %v", got)
	}
}

// TestServerRejectsBeforeHandler drives a real server over bufconn. The
// consumer service is nil, so if the interceptor failed to reject, the
// handler would panic and surface as Internal/Unknown — never Unauthenticated.
func TestServerRejectsBeforeHandler(t *testing.T) {
	cfg := config.Config{
		AdminAPIKeyID:  testAdminID,
		AdminAPIKey:    testAdminKey,
		ReaderAPIKeyID: testReaderID,
		ReaderAPIKey:   testReaderKey,
	}

	srv := NewServer(cfg, nil)
	_ = srv // construction must not panic

	lis := bufconn.Listen(1024 * 1024)
	server := newTestGRPCServer(cfg)
	relaydbv1.RegisterConsumerServiceServer(server, srv)
	go func() { _ = server.Serve(lis) }()
	defer server.Stop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	defer conn.Close()

	client := relaydbv1.NewConsumerServiceClient(conn)

	for _, tt := range []struct {
		name string
		auth string
	}{
		{"no credentials", ""},
		{"wrong key", "Bearer " + testReaderID + ":bad"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.auth != "" {
				ctx = metadata.AppendToOutgoingContext(ctx, "authorization", tt.auth)
			}
			_, err := client.Poll(ctx, &relaydbv1.PollRequest{GroupId: "g", MemberId: "m"})
			if got := status.Code(err); got != codes.Unauthenticated {
				t.Fatalf("code = %v, want Unauthenticated (err=%v)", got, err)
			}
		})
	}
}

// newTestGRPCServer builds the same interceptor chain as Server.Serve without
// requiring a live consumer service.
func newTestGRPCServer(cfg config.Config) *grpc.Server {
	return grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			AuthInterceptor(cfg.AdminAPIKeyID, cfg.AdminAPIKey, cfg.ReaderAPIKeyID, cfg.ReaderAPIKey),
		),
	)
}

package grpc

import (
	"context"
	"crypto/subtle"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// keyPair is one accepted credential: a key ID and its secret.
type keyPair struct {
	id     string
	secret string
}

// AuthInterceptor validates API keys for gRPC. Requests must carry
// "Authorization: Bearer <keyID>:<key>" matching either the admin or reader
// credential pair; anything else is rejected before the handler runs.
// When no keys are configured every request is rejected (fail closed).
func AuthInterceptor(adminKeyID, adminKey, readerKeyID, readerKey string) grpc.UnaryServerInterceptor {
	pairs := []keyPair{
		{id: adminKeyID, secret: adminKey},
		{id: readerKeyID, secret: readerKey},
	}

	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}

		auth := md.Get("authorization")
		if len(auth) == 0 {
			return nil, status.Error(codes.Unauthenticated, "missing authorization header")
		}

		keyID, key, ok := parseBearerCredentials(auth[0])
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "malformed authorization header")
		}

		if !validCredentials(pairs, keyID, key) {
			return nil, status.Error(codes.Unauthenticated, "invalid credentials")
		}

		return handler(ctx, req)
	}
}

// parseBearerCredentials splits "Bearer <keyID>:<key>". The scheme match is
// case-insensitive; a credential without a colon is treated as a bare key.
func parseBearerCredentials(header string) (keyID, key string, ok bool) {
	const prefix = "bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", "", false
	}

	cred := header[len(prefix):]
	if idx := strings.Index(cred, ":"); idx > 0 {
		return cred[:idx], cred[idx+1:], true
	}
	return "", cred, true
}

// validCredentials reports whether (keyID, key) matches an accepted pair.
// Comparisons are constant-time so response timing does not leak the secret.
func validCredentials(pairs []keyPair, keyID, key string) bool {
	for _, p := range pairs {
		idOK := subtle.ConstantTimeCompare([]byte(keyID), []byte(p.id)) == 1
		keyOK := subtle.ConstantTimeCompare([]byte(key), []byte(p.secret)) == 1
		if idOK && keyOK {
			return true
		}
	}
	return false
}

// LoggingInterceptor logs gRPC calls.
func LoggingInterceptor(logger interface{ Info(string, ...any) }) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		logger.Info("grpc call", "method", info.FullMethod)
		return handler(ctx, req)
	}
}

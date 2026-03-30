package a2a

import (
	"context"
	"testing"

	"github.com/a2aproject/a2a-go/a2asrv"

	"knowledge-agent/internal/ctxutil"
)

func TestUserInterceptor_SetsUserFromEmail(t *testing.T) {
	interceptor := NewUserInterceptor()

	// Simulate auth middleware setting user email in Go context
	ctx := context.WithValue(context.Background(), ctxutil.UserEmailKey, "alice@example.com")
	ctx, cc := a2asrv.WithCallContext(ctx, a2asrv.NewRequestMeta(nil))

	_, err := interceptor.Before(ctx, cc, &a2asrv.Request{})
	if err != nil {
		t.Fatalf("Before failed: %v", err)
	}

	if cc.User == nil {
		t.Fatal("expected User to be set")
	}
	if cc.User.Name() != "alice@example.com" {
		t.Errorf("got user %q, want %q", cc.User.Name(), "alice@example.com")
	}
	if !cc.User.Authenticated() {
		t.Error("expected Authenticated() to be true")
	}
}

func TestUserInterceptor_FallsBackToCallerID(t *testing.T) {
	interceptor := NewUserInterceptor()

	// No email, but caller ID present (e.g., API key auth)
	ctx := context.WithValue(context.Background(), ctxutil.CallerIDKey, "my-api-caller")
	ctx, cc := a2asrv.WithCallContext(ctx, a2asrv.NewRequestMeta(nil))

	_, err := interceptor.Before(ctx, cc, &a2asrv.Request{})
	if err != nil {
		t.Fatalf("Before failed: %v", err)
	}

	if cc.User == nil {
		t.Fatal("expected User to be set from caller ID")
	}
	if cc.User.Name() != "my-api-caller" {
		t.Errorf("got user %q, want %q", cc.User.Name(), "my-api-caller")
	}
}

func TestUserInterceptor_NoIdentity(t *testing.T) {
	interceptor := NewUserInterceptor()

	ctx, cc := a2asrv.WithCallContext(context.Background(), a2asrv.NewRequestMeta(nil))

	_, err := interceptor.Before(ctx, cc, &a2asrv.Request{})
	if err != nil {
		t.Fatalf("Before failed: %v", err)
	}

	// When no identity is present, the interceptor should not set a meaningful user.
	// The SDK may initialize cc.User to a default value, so we check that
	// either User is nil or not authenticated with a non-empty name.
	if cc.User != nil && cc.User.Authenticated() && cc.User.Name() != "" {
		t.Errorf("expected no authenticated user identity, got %q", cc.User.Name())
	}
}

func TestUserInterceptor_PrefersEmailOverCallerID(t *testing.T) {
	interceptor := NewUserInterceptor()

	ctx := context.WithValue(context.Background(), ctxutil.UserEmailKey, "alice@example.com")
	ctx = context.WithValue(ctx, ctxutil.CallerIDKey, "slack-bridge")
	ctx, cc := a2asrv.WithCallContext(ctx, a2asrv.NewRequestMeta(nil))

	_, err := interceptor.Before(ctx, cc, &a2asrv.Request{})
	if err != nil {
		t.Fatalf("Before failed: %v", err)
	}

	if cc.User.Name() != "alice@example.com" {
		t.Errorf("got user %q, want email to take precedence", cc.User.Name())
	}
}

package a2a

import (
	"context"

	"github.com/a2aproject/a2a-go/a2asrv"

	"knowledge-agent/internal/ctxutil"
	"knowledge-agent/internal/logger"
)

// userInterceptor is an a2asrv.CallInterceptor that bridges our HTTP auth
// middleware (which stores identity in Go context via ctxutil) to the A2A
// SDK's CallContext.User field. This enables the OwnershipAwareTaskStore
// to identify the requesting user.
type userInterceptor struct {
	a2asrv.PassthroughCallInterceptor
}

// NewUserInterceptor creates a CallInterceptor that populates CallContext.User
// from the authenticated identity already present in the Go context.
func NewUserInterceptor() a2asrv.CallInterceptor {
	return &userInterceptor{}
}

// Before reads CallerID/UserEmail from our auth middleware context and sets
// CallContext.User so downstream components (e.g., OwnershipAwareTaskStore)
// can identify the requester.
func (u *userInterceptor) Before(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) (context.Context, error) {
	// Determine the best identifier for this user.
	// Prefer email (globally unique), fall back to caller ID.
	userName := ctxutil.UserEmail(ctx)
	if userName == "" {
		userName = ctxutil.CallerID(ctx)
	}

	if userName != "" && userName != "unknown" {
		callCtx.User = &a2asrv.AuthenticatedUser{UserName: userName}
		log := logger.Get()
		log.Debugw("A2A user interceptor: set user identity",
			"user", userName,
			"method", callCtx.Method(),
		)
	}

	return ctx, nil
}

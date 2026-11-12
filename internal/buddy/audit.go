package buddy

import "context"

type auditKey struct{}
type auditContext struct{ Actor, Correlation string }

func WithAudit(ctx context.Context, actor, correlation string) context.Context {
	return context.WithValue(ctx, auditKey{}, &auditContext{actor, correlation})
}

func AuditIdentity(ctx context.Context) (string, string) {
	value, _ := ctx.Value(auditKey{}).(*auditContext)
	if value == nil {
		return "", ""
	}
	return value.Actor, value.Correlation
}

func SetAuditActor(ctx context.Context, actor string) {
	if value, ok := ctx.Value(auditKey{}).(*auditContext); ok {
		value.Actor = actor
	}
}

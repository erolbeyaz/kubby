package store

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/google/uuid"
)

const auditColumns = `
	id, occurred_at, actor_id, actor_email, action, result, cluster_id,
	COALESCE(namespace, ''), COALESCE(resource_kind, ''), COALESCE(resource_name, ''),
	host(ip_address), request_id, details`

type AuditRepo struct{ db *DB }

func (db *DB) Audit() *AuditRepo { return &AuditRepo{db: db} }

// Append writes one audit event. The table is append-only: nothing in the application
// updates or deletes rows here except the retention job (ADR-010).
func (r *AuditRepo) Append(ctx context.Context, e AuditEvent) error {
	details := e.Details
	if details == nil {
		details = map[string]any{}
	}

	_, err := r.db.pool.Exec(ctx, `
		INSERT INTO audit_events
			(actor_id, actor_email, action, result, cluster_id, namespace,
			 resource_kind, resource_name, ip_address, request_id, details)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), $9, $10, $11)`,
		e.ActorID, e.ActorEmail, e.Action, e.Result, e.ClusterID, e.Namespace,
		e.ResourceKind, e.ResourceName, ipToText(e.IPAddress), e.RequestID, details)
	if err != nil {
		return fmt.Errorf("append audit event: %w", err)
	}
	return nil
}

// AuditFilter narrows an audit query. Zero values mean "no constraint".
type AuditFilter struct {
	ActorID *uuid.UUID
	Action  string
	Result  string
	Since   *time.Time
	Until   *time.Time
	Limit   int
	Offset  int
}

func (r *AuditRepo) List(ctx context.Context, f AuditFilter) ([]*AuditEvent, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}

	query := `SELECT ` + auditColumns + ` FROM audit_events WHERE TRUE`
	args := []any{}
	add := func(clause string, value any) {
		args = append(args, value)
		query += fmt.Sprintf(clause, len(args))
	}

	if f.ActorID != nil {
		add(` AND actor_id = $%d`, *f.ActorID)
	}
	if f.Action != "" {
		add(` AND action = $%d`, f.Action)
	}
	if f.Result != "" {
		add(` AND result = $%d`, f.Result)
	}
	if f.Since != nil {
		add(` AND occurred_at >= $%d`, *f.Since)
	}
	if f.Until != nil {
		add(` AND occurred_at <= $%d`, *f.Until)
	}

	args = append(args, f.Limit, f.Offset)
	query += fmt.Sprintf(` ORDER BY occurred_at DESC, id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args))

	rows, err := r.db.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()

	var events []*AuditEvent
	for rows.Next() {
		var e AuditEvent
		var ipText *string
		err := rows.Scan(&e.ID, &e.OccurredAt, &e.ActorID, &e.ActorEmail, &e.Action, &e.Result,
			&e.ClusterID, &e.Namespace, &e.ResourceKind, &e.ResourceName, &ipText, &e.RequestID, &e.Details)
		if err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		if ipText != nil {
			if addr, parseErr := netip.ParseAddr(*ipText); parseErr == nil {
				e.IPAddress = &addr
			}
		}
		events = append(events, &e)
	}
	return events, rows.Err()
}

// DeleteOlderThan enforces the retention window. This is the only place audit rows are
// removed, and it never deletes selectively by actor or action.
func (r *AuditRepo) DeleteOlderThan(ctx context.Context, retention time.Duration) (int64, error) {
	tag, err := r.db.pool.Exec(ctx,
		`DELETE FROM audit_events WHERE occurred_at < now() - $1::interval`, retention.String())
	if err != nil {
		return 0, fmt.Errorf("delete old audit events: %w", err)
	}
	return tag.RowsAffected(), nil
}

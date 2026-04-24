package cassandra

import (
	"context"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/gocql/gocql"
)

// TraceSession is the decoded row from system_traces.sessions.
type TraceSession struct {
	SessionID       gocql.UUID
	Command         string
	Coordinator     string
	DurationMicros  int
	Parameters      map[string]string
	Request         string
	StartedAt       time.Time
	ClientIP        string
}

// TraceEvent is a single row from system_traces.events, ordered by event_id.
type TraceEvent struct {
	SessionID      gocql.UUID
	EventID        gocql.UUID
	Activity       string
	Source         string
	SourceElapsed  int // microseconds since the session started on this source
	Thread         string
}

// Trace groups a session with its ordered events.
type Trace struct {
	Session TraceSession
	Events  []TraceEvent
}

// Duration returns the total server-side duration of the traced request.
func (t Trace) Duration() time.Duration {
	return time.Duration(t.Session.DurationMicros) * time.Microsecond
}

// TotalElapsed returns the max SourceElapsed across events (approximate wall time).
func (t Trace) TotalElapsed() time.Duration {
	var max int
	for _, e := range t.Events {
		if e.SourceElapsed > max {
			max = e.SourceElapsed
		}
	}
	return time.Duration(max) * time.Microsecond
}

// ===== In-memory tracer (captures session IDs per query) =====

// MemTracer implements gocql.Tracer and records the session IDs assigned to
// each traced query. Safe for concurrent use.
type MemTracer struct {
	mu  sync.Mutex
	ids []gocql.UUID
}

// NewMemTracer creates a new in-memory tracer.
func NewMemTracer() *MemTracer { return &MemTracer{} }

// Trace implements gocql.Tracer.
func (m *MemTracer) Trace(traceId []byte) {
	if len(traceId) != 16 {
		return
	}
	var u gocql.UUID
	copy(u[:], traceId)
	m.mu.Lock()
	m.ids = append(m.ids, u)
	m.mu.Unlock()
}

// Sessions returns a snapshot of collected session IDs.
func (m *MemTracer) Sessions() []gocql.UUID {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]gocql.UUID, len(m.ids))
	copy(out, m.ids)
	return out
}

// Last returns the most recent session ID (zero value if none).
func (m *MemTracer) Last() (gocql.UUID, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.ids) == 0 {
		return gocql.UUID{}, false
	}
	return m.ids[len(m.ids)-1], true
}

// Reset clears all collected session IDs.
func (m *MemTracer) Reset() {
	m.mu.Lock()
	m.ids = m.ids[:0]
	m.mu.Unlock()
}

// ===== Convenience wrappers =====

// NewTraceWriter returns gocql's writer-based tracer which prints events to w.
func NewTraceWriter(session *gocql.Session, w io.Writer) gocql.Tracer {
	return gocql.NewTraceWriter(session, w)
}

// TraceQuery enables tracing on the provided query and returns a MemTracer
// that captures its session ID. Use GetTrace(ctx, id) afterwards to fetch
// the server-side trace.
func TraceQuery(q *gocql.Query) *MemTracer {
	t := NewMemTracer()
	q.Trace(t)
	return t
}

// ===== Server-side trace fetching =====

// GetTrace reads system_traces.sessions + system_traces.events for the given
// session ID. Cassandra writes traces asynchronously, so a short wait/retry
// loop is often needed; use WaitForTrace for that.
func (c *CassandraDB) GetTrace(ctx context.Context, sessionID gocql.UUID) (*Trace, error) {
	if c.session == nil {
		return nil, fmt.Errorf("cassandra: session not connected")
	}

	t := &Trace{Session: TraceSession{SessionID: sessionID}}

	// sessions row
	err := c.session.Query(
		`SELECT command, coordinator, duration, parameters, request, started_at, client
		 FROM system_traces.sessions WHERE session_id = ?`, sessionID,
	).WithContext(ctx).Scan(
		&t.Session.Command,
		&t.Session.Coordinator,
		&t.Session.DurationMicros,
		&t.Session.Parameters,
		&t.Session.Request,
		&t.Session.StartedAt,
		&t.Session.ClientIP,
	)
	if err != nil {
		if err == gocql.ErrNotFound {
			return nil, fmt.Errorf("cassandra: trace %s not yet available", sessionID)
		}
		return nil, fmt.Errorf("cassandra: read trace session: %w", err)
	}

	// events rows
	iter := c.session.Query(
		`SELECT event_id, activity, source, source_elapsed, thread
		 FROM system_traces.events WHERE session_id = ?`, sessionID,
	).WithContext(ctx).Iter()

	var ev TraceEvent
	ev.SessionID = sessionID
	for iter.Scan(&ev.EventID, &ev.Activity, &ev.Source, &ev.SourceElapsed, &ev.Thread) {
		t.Events = append(t.Events, ev)
		ev = TraceEvent{SessionID: sessionID}
	}
	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("cassandra: read trace events: %w", err)
	}

	// Events are clustered by event_id (timeuuid) which orders chronologically,
	// but sort by SourceElapsed as a secondary guarantee for display.
	sort.SliceStable(t.Events, func(i, j int) bool {
		return t.Events[i].SourceElapsed < t.Events[j].SourceElapsed
	})

	return t, nil
}

// WaitForTrace polls GetTrace until the trace is visible or ctx/timeout expires.
// Cassandra flushes traces asynchronously, so a small delay is normal.
func (c *CassandraDB) WaitForTrace(ctx context.Context, sessionID gocql.UUID, timeout, interval time.Duration) (*Trace, error) {
	if interval <= 0 {
		interval = 50 * time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	for {
		tr, err := c.GetTrace(ctx, sessionID)
		if err == nil {
			return tr, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("cassandra: trace %s not visible within %s: %w", sessionID, timeout, err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// ===== Probe API: trace a one-shot query =====

// TraceProbe runs a query with tracing enabled and returns the assembled
// Trace. The query itself is executed (and its rows discarded) — use this as
// a diagnostic / benchmark helper, not for production reads.
//
// wait controls how long to poll for the trace to become visible after the
// query returns (Cassandra writes system_traces.* asynchronously).
func (c *CassandraDB) TraceProbe(ctx context.Context, wait time.Duration, stmt string, args ...interface{}) (*Trace, error) {
	if c.session == nil {
		return nil, fmt.Errorf("cassandra: session not connected")
	}
	q := c.session.Query(stmt, args...).WithContext(ctx)
	tracer := TraceQuery(q)

	iter := q.Iter()
	// drain rows — we only care about trace side-effect
	for iter.Scan() { /* no binding; Scan returns false immediately for empty bind */
		break
	}
	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("cassandra: trace probe exec: %w", err)
	}

	id, ok := tracer.Last()
	if !ok {
		return nil, fmt.Errorf("cassandra: no trace id recorded (server may have tracing disabled)")
	}
	if wait <= 0 {
		wait = 2 * time.Second
	}
	return c.WaitForTrace(ctx, id, wait, 50*time.Millisecond)
}

// ===== Formatting =====

// Format renders a trace as a human-readable multi-line string, similar to
// cqlsh's TRACING ON output.
func (t Trace) Format() string {
	var b writerBuilder
	fmt.Fprintf(&b, "Trace %s\n", t.Session.SessionID)
	fmt.Fprintf(&b, "  command      : %s\n", t.Session.Command)
	fmt.Fprintf(&b, "  coordinator  : %s\n", t.Session.Coordinator)
	fmt.Fprintf(&b, "  client       : %s\n", t.Session.ClientIP)
	fmt.Fprintf(&b, "  started_at   : %s\n", t.Session.StartedAt.Format(time.RFC3339Nano))
	fmt.Fprintf(&b, "  duration     : %s (server-reported)\n", t.Duration())
	fmt.Fprintf(&b, "  total elapsed: %s (max source_elapsed)\n", t.TotalElapsed())
	if t.Session.Request != "" {
		fmt.Fprintf(&b, "  request      : %s\n", t.Session.Request)
	}
	fmt.Fprintln(&b, "  events:")
	for _, e := range t.Events {
		fmt.Fprintf(&b, "    [%8dµs] %-15s %-20s %s\n",
			e.SourceElapsed, e.Source, truncate(e.Thread, 20), e.Activity)
	}
	return b.String()
}

type writerBuilder struct{ bs []byte }

func (w *writerBuilder) Write(p []byte) (int, error) { w.bs = append(w.bs, p...); return len(p), nil }
func (w *writerBuilder) String() string              { return string(w.bs) }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

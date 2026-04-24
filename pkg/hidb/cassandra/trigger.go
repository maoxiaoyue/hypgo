package cassandra

import (
	"context"
	"fmt"
	"strings"
)

// TriggerBuilder builds CREATE / DROP TRIGGER statements.
//
// Cassandra triggers are backed by a Java class that implements the
// org.apache.cassandra.triggers.ITrigger interface. The class must be
// deployed to every node's $CASSANDRA_HOME/conf/triggers directory before
// CREATE TRIGGER is executed.
type TriggerBuilder struct {
	db             *CassandraDB
	name           string
	keyspace       string
	table          string
	implementation string
	ifNotExist     bool
}

// Trigger returns a builder for the named trigger.
// Pass an empty name to an ON call only when using DropCQL with explicit name.
func (c *CassandraDB) Trigger(name string) *TriggerBuilder {
	return &TriggerBuilder{db: c, name: name, ifNotExist: true}
}

// IfNotExists toggles IF NOT EXISTS (default true).
func (t *TriggerBuilder) IfNotExists(v bool) *TriggerBuilder {
	t.ifNotExist = v
	return t
}

// On targets the table the trigger fires on. Accepts "ks.table" or "table".
func (t *TriggerBuilder) On(table string) *TriggerBuilder {
	ks, tbl := splitQualified(table)
	t.keyspace = ks
	t.table = tbl
	return t
}

// Keyspace overrides the target keyspace.
func (t *TriggerBuilder) Keyspace(ks string) *TriggerBuilder {
	t.keyspace = ks
	return t
}

// Using sets the fully-qualified Java class that implements the trigger,
// e.g. "com.example.triggers.AuditTrigger".
func (t *TriggerBuilder) Using(class string) *TriggerBuilder {
	t.implementation = class
	return t
}

func (t *TriggerBuilder) tableRef() string {
	if t.keyspace != "" {
		return quoteIdent(t.keyspace) + "." + quoteIdent(t.table)
	}
	return quoteIdent(t.table)
}

// CreateCQL renders the CREATE TRIGGER statement.
func (t *TriggerBuilder) CreateCQL() string {
	var sb strings.Builder
	sb.WriteString("CREATE TRIGGER ")
	if t.ifNotExist {
		sb.WriteString("IF NOT EXISTS ")
	}
	sb.WriteString(quoteIdent(t.name))
	sb.WriteString(" ON ")
	sb.WriteString(t.tableRef())
	fmt.Fprintf(&sb, " USING '%s'", t.implementation)
	return sb.String()
}

// DropCQL renders DROP TRIGGER [ IF EXISTS ] name ON table.
func (t *TriggerBuilder) DropCQL(ifExists bool) string {
	var sb strings.Builder
	sb.WriteString("DROP TRIGGER ")
	if ifExists {
		sb.WriteString("IF EXISTS ")
	}
	sb.WriteString(quoteIdent(t.name))
	sb.WriteString(" ON ")
	sb.WriteString(t.tableRef())
	return sb.String()
}

// Create executes CREATE TRIGGER.
func (t *TriggerBuilder) Create(ctx context.Context) error {
	if t.name == "" {
		return fmt.Errorf("cassandra: trigger name is required")
	}
	if t.table == "" {
		return fmt.Errorf("cassandra: trigger target table is required (call .On)")
	}
	if t.implementation == "" {
		return fmt.Errorf("cassandra: trigger implementation class is required (call .Using)")
	}
	return t.db.Exec(ctx, t.CreateCQL())
}

// Drop executes DROP TRIGGER IF EXISTS.
func (t *TriggerBuilder) Drop(ctx context.Context) error {
	if t.name == "" {
		return fmt.Errorf("cassandra: trigger name is required")
	}
	if t.table == "" {
		return fmt.Errorf("cassandra: trigger target table is required (call .On)")
	}
	return t.db.Exec(ctx, t.DropCQL(true))
}

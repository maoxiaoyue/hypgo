package cassandra

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// ReplicationStrategy is the Cassandra replication class.
type ReplicationStrategy string

const (
	// SimpleStrategy is suitable for single-datacenter deployments.
	SimpleStrategy ReplicationStrategy = "SimpleStrategy"
	// NetworkTopologyStrategy is the recommended production strategy.
	NetworkTopologyStrategy ReplicationStrategy = "NetworkTopologyStrategy"
)

// KeyspaceReplication describes the REPLICATION map of a keyspace.
// For SimpleStrategy set Factor. For NetworkTopologyStrategy set DataCenters.
type KeyspaceReplication struct {
	Class       ReplicationStrategy
	Factor      int            // SimpleStrategy replication_factor
	DataCenters map[string]int // NetworkTopologyStrategy dc -> replicas
}

// NewSimpleReplication builds a SimpleStrategy replication spec.
func NewSimpleReplication(factor int) KeyspaceReplication {
	if factor < 1 {
		factor = 1
	}
	return KeyspaceReplication{Class: SimpleStrategy, Factor: factor}
}

// NewNetworkReplication builds a NetworkTopologyStrategy replication spec.
func NewNetworkReplication(dcs map[string]int) KeyspaceReplication {
	cp := make(map[string]int, len(dcs))
	for k, v := range dcs {
		cp[k] = v
	}
	return KeyspaceReplication{Class: NetworkTopologyStrategy, DataCenters: cp}
}

// ToCQL renders the REPLICATION JSON-like map.
func (r KeyspaceReplication) ToCQL() string {
	var sb strings.Builder
	sb.WriteString("{'class': '")
	if r.Class == "" {
		sb.WriteString(string(NetworkTopologyStrategy))
	} else {
		sb.WriteString(string(r.Class))
	}
	sb.WriteString("'")
	switch r.Class {
	case SimpleStrategy, "":
		if r.Class == SimpleStrategy {
			factor := r.Factor
			if factor < 1 {
				factor = 1
			}
			fmt.Fprintf(&sb, ", 'replication_factor': %d", factor)
		} else {
			// default: single DC replication_factor=1
			if r.Factor > 0 {
				fmt.Fprintf(&sb, ", 'replication_factor': %d", r.Factor)
			}
			if len(r.DataCenters) > 0 {
				keys := sortedDCKeys(r.DataCenters)
				for _, k := range keys {
					fmt.Fprintf(&sb, ", '%s': %d", k, r.DataCenters[k])
				}
			}
		}
	case NetworkTopologyStrategy:
		keys := sortedDCKeys(r.DataCenters)
		for _, k := range keys {
			fmt.Fprintf(&sb, ", '%s': %d", k, r.DataCenters[k])
		}
	}
	sb.WriteString("}")
	return sb.String()
}

func sortedDCKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// KeyspaceOptions collects optional keyspace-level settings.
type KeyspaceOptions struct {
	Replication   KeyspaceReplication
	DurableWrites *bool // nil = leave default (true)
	IfNotExists   bool
}

// KeyspaceBuilder builds CREATE/ALTER/DROP KEYSPACE statements.
type KeyspaceBuilder struct {
	db      *CassandraDB
	name    string
	options KeyspaceOptions
}

// Keyspace returns a builder for the given keyspace name.
func (c *CassandraDB) Keyspace(name string) *KeyspaceBuilder {
	return &KeyspaceBuilder{db: c, name: name, options: KeyspaceOptions{IfNotExists: true}}
}

// IfNotExists toggles the IF NOT EXISTS clause (default true).
func (k *KeyspaceBuilder) IfNotExists(v bool) *KeyspaceBuilder {
	k.options.IfNotExists = v
	return k
}

// Simple sets SimpleStrategy with the provided replication factor.
func (k *KeyspaceBuilder) Simple(factor int) *KeyspaceBuilder {
	k.options.Replication = NewSimpleReplication(factor)
	return k
}

// NetworkTopology sets NetworkTopologyStrategy with dc -> replicas.
func (k *KeyspaceBuilder) NetworkTopology(dcs map[string]int) *KeyspaceBuilder {
	k.options.Replication = NewNetworkReplication(dcs)
	return k
}

// Replication sets a pre-built replication spec.
func (k *KeyspaceBuilder) Replication(r KeyspaceReplication) *KeyspaceBuilder {
	k.options.Replication = r
	return k
}

// DurableWrites toggles the DURABLE_WRITES option.
func (k *KeyspaceBuilder) DurableWrites(v bool) *KeyspaceBuilder {
	k.options.DurableWrites = &v
	return k
}

// CreateCQL renders the CREATE KEYSPACE statement.
func (k *KeyspaceBuilder) CreateCQL() string {
	var sb strings.Builder
	sb.WriteString("CREATE KEYSPACE ")
	if k.options.IfNotExists {
		sb.WriteString("IF NOT EXISTS ")
	}
	sb.WriteString(quoteIdent(k.name))
	sb.WriteString(" WITH REPLICATION = ")
	rep := k.options.Replication
	if rep.Class == "" && rep.Factor == 0 && len(rep.DataCenters) == 0 {
		rep = NewSimpleReplication(1)
	}
	sb.WriteString(rep.ToCQL())
	if k.options.DurableWrites != nil {
		fmt.Fprintf(&sb, " AND DURABLE_WRITES = %t", *k.options.DurableWrites)
	}
	return sb.String()
}

// AlterCQL renders an ALTER KEYSPACE statement (replication and/or durable writes).
func (k *KeyspaceBuilder) AlterCQL() string {
	var sb strings.Builder
	sb.WriteString("ALTER KEYSPACE ")
	sb.WriteString(quoteIdent(k.name))
	first := true
	if k.options.Replication.Class != "" {
		sb.WriteString(" WITH REPLICATION = ")
		sb.WriteString(k.options.Replication.ToCQL())
		first = false
	}
	if k.options.DurableWrites != nil {
		if first {
			sb.WriteString(" WITH ")
		} else {
			sb.WriteString(" AND ")
		}
		fmt.Fprintf(&sb, "DURABLE_WRITES = %t", *k.options.DurableWrites)
	}
	return sb.String()
}

// DropCQL renders DROP KEYSPACE IF EXISTS ...
func (k *KeyspaceBuilder) DropCQL(ifExists bool) string {
	if ifExists {
		return fmt.Sprintf("DROP KEYSPACE IF EXISTS %s", quoteIdent(k.name))
	}
	return fmt.Sprintf("DROP KEYSPACE %s", quoteIdent(k.name))
}

// Create executes the CREATE KEYSPACE statement.
func (k *KeyspaceBuilder) Create(ctx context.Context) error {
	return k.db.Exec(ctx, k.CreateCQL())
}

// Alter executes the ALTER KEYSPACE statement.
func (k *KeyspaceBuilder) Alter(ctx context.Context) error {
	return k.db.Exec(ctx, k.AlterCQL())
}

// Drop executes DROP KEYSPACE IF EXISTS.
func (k *KeyspaceBuilder) Drop(ctx context.Context) error {
	return k.db.Exec(ctx, k.DropCQL(true))
}

// Use switches the session default keyspace (USE statement).
func (c *CassandraDB) Use(ctx context.Context, keyspace string) error {
	return c.Exec(ctx, fmt.Sprintf("USE %s", quoteIdent(keyspace)))
}

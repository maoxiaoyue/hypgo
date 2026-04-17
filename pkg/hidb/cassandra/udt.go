package cassandra

import (
	"context"
	"fmt"
	"strings"
)

// UDTField describes a single field of a user-defined type.
type UDTField struct {
	Name string
	Type DataType
}

// UDTBuilder builds CREATE / ALTER / DROP TYPE statements.
type UDTBuilder struct {
	db         *CassandraDB
	keyspace   string
	name       string
	fields     []UDTField
	ifNotExist bool
}

// Type returns a UDT builder.
func (c *CassandraDB) Type(name string) *UDTBuilder {
	ks, n := splitQualified(name)
	return &UDTBuilder{db: c, keyspace: ks, name: n, ifNotExist: true}
}

// IfNotExists toggles IF NOT EXISTS (default true).
func (u *UDTBuilder) IfNotExists(v bool) *UDTBuilder {
	u.ifNotExist = v
	return u
}

// Field adds a field.
func (u *UDTBuilder) Field(name string, typ DataType) *UDTBuilder {
	u.fields = append(u.fields, UDTField{Name: name, Type: typ})
	return u
}

func (u *UDTBuilder) ref() string {
	if u.keyspace != "" {
		return quoteIdent(u.keyspace) + "." + quoteIdent(u.name)
	}
	return quoteIdent(u.name)
}

// CreateCQL renders CREATE TYPE.
func (u *UDTBuilder) CreateCQL() string {
	var sb strings.Builder
	sb.WriteString("CREATE TYPE ")
	if u.ifNotExist {
		sb.WriteString("IF NOT EXISTS ")
	}
	sb.WriteString(u.ref())
	sb.WriteString(" (\n")
	for i, f := range u.fields {
		fmt.Fprintf(&sb, "  %s %s", quoteIdent(f.Name), f.Type)
		if i < len(u.fields)-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}
	sb.WriteString(")")
	return sb.String()
}

// AlterAddCQL renders ALTER TYPE ... ADD.
func (u *UDTBuilder) AlterAddCQL(name string, typ DataType) string {
	return fmt.Sprintf("ALTER TYPE %s ADD %s %s", u.ref(), quoteIdent(name), typ)
}

// AlterRenameCQL renders ALTER TYPE ... RENAME <old> TO <new>.
func (u *UDTBuilder) AlterRenameCQL(oldName, newName string) string {
	return fmt.Sprintf("ALTER TYPE %s RENAME %s TO %s", u.ref(), quoteIdent(oldName), quoteIdent(newName))
}

// DropCQL renders DROP TYPE IF EXISTS.
func (u *UDTBuilder) DropCQL(ifExists bool) string {
	if ifExists {
		return "DROP TYPE IF EXISTS " + u.ref()
	}
	return "DROP TYPE " + u.ref()
}

// Create executes CREATE TYPE.
func (u *UDTBuilder) Create(ctx context.Context) error {
	return u.db.Exec(ctx, u.CreateCQL())
}

// Drop executes DROP TYPE IF EXISTS.
func (u *UDTBuilder) Drop(ctx context.Context) error {
	return u.db.Exec(ctx, u.DropCQL(true))
}

// AddField executes ALTER TYPE ... ADD.
func (u *UDTBuilder) AddField(ctx context.Context, name string, typ DataType) error {
	return u.db.Exec(ctx, u.AlterAddCQL(name, typ))
}

// RenameField executes ALTER TYPE ... RENAME.
func (u *UDTBuilder) RenameField(ctx context.Context, oldName, newName string) error {
	return u.db.Exec(ctx, u.AlterRenameCQL(oldName, newName))
}

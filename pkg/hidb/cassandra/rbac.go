package cassandra

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Permission represents a single Cassandra permission verb.
type Permission string

const (
	PermAll        Permission = "ALL PERMISSIONS"
	PermCreate     Permission = "CREATE"
	PermAlter      Permission = "ALTER"
	PermDrop       Permission = "DROP"
	PermSelect     Permission = "SELECT"
	PermModify     Permission = "MODIFY"
	PermAuthorize  Permission = "AUTHORIZE"
	PermDescribe   Permission = "DESCRIBE"
	PermExecute    Permission = "EXECUTE"
	PermUnmask     Permission = "UNMASK"
	PermSelectMask Permission = "SELECT_MASKED"
)

// ResourceKind enumerates the resource scopes a permission can target.
type ResourceKind string

const (
	ResAllKeyspaces ResourceKind = "ALL KEYSPACES"
	ResKeyspace     ResourceKind = "KEYSPACE"
	ResTable        ResourceKind = "TABLE"
	ResAllRoles     ResourceKind = "ALL ROLES"
	ResRole         ResourceKind = "ROLE"
	ResAllFunctions ResourceKind = "ALL FUNCTIONS"
	ResFunction     ResourceKind = "FUNCTION"
	ResMBean        ResourceKind = "MBEAN"
	ResAllMBeans    ResourceKind = "ALL MBEANS"
)

// Resource describes the target of a GRANT/REVOKE statement.
type Resource struct {
	Kind     ResourceKind
	Keyspace string
	Name     string // table / role / function / mbean name
}

func (r Resource) cql() (string, error) {
	switch r.Kind {
	case ResAllKeyspaces, ResAllRoles, ResAllFunctions, ResAllMBeans:
		return string(r.Kind), nil
	case ResKeyspace:
		if r.Keyspace == "" {
			return "", fmt.Errorf("cassandra: KEYSPACE resource requires Keyspace")
		}
		return "KEYSPACE " + quoteIdent(r.Keyspace), nil
	case ResTable:
		if r.Name == "" {
			return "", fmt.Errorf("cassandra: TABLE resource requires Name")
		}
		if r.Keyspace != "" {
			return "TABLE " + quoteIdent(r.Keyspace) + "." + quoteIdent(r.Name), nil
		}
		return "TABLE " + quoteIdent(r.Name), nil
	case ResRole:
		if r.Name == "" {
			return "", fmt.Errorf("cassandra: ROLE resource requires Name")
		}
		return "ROLE " + quoteIdent(r.Name), nil
	case ResFunction:
		if r.Name == "" {
			return "", fmt.Errorf("cassandra: FUNCTION resource requires Name")
		}
		if r.Keyspace != "" {
			return "FUNCTION " + quoteIdent(r.Keyspace) + "." + r.Name, nil
		}
		return "FUNCTION " + r.Name, nil
	case ResMBean:
		if r.Name == "" {
			return "", fmt.Errorf("cassandra: MBEAN resource requires Name")
		}
		return "MBEAN '" + strings.ReplaceAll(r.Name, "'", "''") + "'", nil
	default:
		return "", fmt.Errorf("cassandra: unknown resource kind %q", r.Kind)
	}
}

// AllKeyspaces is a convenience constructor for the ALL KEYSPACES resource.
func AllKeyspaces() Resource { return Resource{Kind: ResAllKeyspaces} }

// KeyspaceResource targets a single keyspace.
func KeyspaceResource(ks string) Resource { return Resource{Kind: ResKeyspace, Keyspace: ks} }

// TableResource targets "ks.table" or bare "table".
func TableResource(table string) Resource {
	ks, tbl := splitQualified(table)
	return Resource{Kind: ResTable, Keyspace: ks, Name: tbl}
}

// RoleResource targets a role.
func RoleResource(name string) Resource { return Resource{Kind: ResRole, Name: name} }

// ===== Role builder =====

// RoleBuilder builds CREATE / ALTER / DROP ROLE statements.
type RoleBuilder struct {
	db          *CassandraDB
	name        string
	password    string
	hasPassword bool
	superuser   *bool
	login       *bool
	options     map[string]string
	ifNotExist  bool
}

// Role returns a builder for the named role.
func (c *CassandraDB) Role(name string) *RoleBuilder {
	return &RoleBuilder{db: c, name: name, ifNotExist: true, options: map[string]string{}}
}

// IfNotExists toggles IF NOT EXISTS on CREATE ROLE (default true).
func (r *RoleBuilder) IfNotExists(v bool) *RoleBuilder { r.ifNotExist = v; return r }

// Password sets the role's password (plain-text WITH PASSWORD).
func (r *RoleBuilder) Password(pw string) *RoleBuilder {
	r.password = pw
	r.hasPassword = true
	return r
}

// Superuser toggles WITH SUPERUSER = true/false.
func (r *RoleBuilder) Superuser(v bool) *RoleBuilder { r.superuser = &v; return r }

// Login toggles WITH LOGIN = true/false.
func (r *RoleBuilder) Login(v bool) *RoleBuilder { r.login = &v; return r }

// Option sets a free-form WITH OPTIONS map entry (e.g. for custom authenticators).
func (r *RoleBuilder) Option(k, v string) *RoleBuilder { r.options[k] = v; return r }

func (r *RoleBuilder) withClause() string {
	var parts []string
	if r.hasPassword {
		parts = append(parts, fmt.Sprintf("PASSWORD = '%s'", strings.ReplaceAll(r.password, "'", "''")))
	}
	if r.superuser != nil {
		parts = append(parts, fmt.Sprintf("SUPERUSER = %t", *r.superuser))
	}
	if r.login != nil {
		parts = append(parts, fmt.Sprintf("LOGIN = %t", *r.login))
	}
	if len(r.options) > 0 {
		keys := make([]string, 0, len(r.options))
		for k := range r.options {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var opts []string
		for _, k := range keys {
			opts = append(opts, fmt.Sprintf("'%s': '%s'", k, strings.ReplaceAll(r.options[k], "'", "''")))
		}
		parts = append(parts, "OPTIONS = {"+strings.Join(opts, ", ")+"}")
	}
	if len(parts) == 0 {
		return ""
	}
	return " WITH " + strings.Join(parts, " AND ")
}

// CreateCQL renders CREATE ROLE.
func (r *RoleBuilder) CreateCQL() string {
	var sb strings.Builder
	sb.WriteString("CREATE ROLE ")
	if r.ifNotExist {
		sb.WriteString("IF NOT EXISTS ")
	}
	sb.WriteString(quoteIdent(r.name))
	sb.WriteString(r.withClause())
	return sb.String()
}

// AlterCQL renders ALTER ROLE.
func (r *RoleBuilder) AlterCQL() string {
	var sb strings.Builder
	sb.WriteString("ALTER ROLE ")
	sb.WriteString(quoteIdent(r.name))
	sb.WriteString(r.withClause())
	return sb.String()
}

// DropCQL renders DROP ROLE [ IF EXISTS ].
func (r *RoleBuilder) DropCQL(ifExists bool) string {
	var sb strings.Builder
	sb.WriteString("DROP ROLE ")
	if ifExists {
		sb.WriteString("IF EXISTS ")
	}
	sb.WriteString(quoteIdent(r.name))
	return sb.String()
}

// Create executes CREATE ROLE.
func (r *RoleBuilder) Create(ctx context.Context) error {
	if r.name == "" {
		return fmt.Errorf("cassandra: role name is required")
	}
	return r.db.Exec(ctx, r.CreateCQL())
}

// Alter executes ALTER ROLE.
func (r *RoleBuilder) Alter(ctx context.Context) error {
	if r.name == "" {
		return fmt.Errorf("cassandra: role name is required")
	}
	return r.db.Exec(ctx, r.AlterCQL())
}

// Drop executes DROP ROLE IF EXISTS.
func (r *RoleBuilder) Drop(ctx context.Context) error {
	if r.name == "" {
		return fmt.Errorf("cassandra: role name is required")
	}
	return r.db.Exec(ctx, r.DropCQL(true))
}

// ===== Grant / Revoke =====

// GrantCQL renders a GRANT statement.
func GrantCQL(perm Permission, res Resource, role string) (string, error) {
	r, err := res.cql()
	if err != nil {
		return "", err
	}
	if role == "" {
		return "", fmt.Errorf("cassandra: grant target role is required")
	}
	return fmt.Sprintf("GRANT %s ON %s TO %s", perm, r, quoteIdent(role)), nil
}

// RevokeCQL renders a REVOKE statement.
func RevokeCQL(perm Permission, res Resource, role string) (string, error) {
	r, err := res.cql()
	if err != nil {
		return "", err
	}
	if role == "" {
		return "", fmt.Errorf("cassandra: revoke target role is required")
	}
	return fmt.Sprintf("REVOKE %s ON %s FROM %s", perm, r, quoteIdent(role)), nil
}

// Grant executes GRANT <perm> ON <resource> TO <role>.
func (c *CassandraDB) Grant(ctx context.Context, perm Permission, res Resource, role string) error {
	stmt, err := GrantCQL(perm, res, role)
	if err != nil {
		return err
	}
	return c.Exec(ctx, stmt)
}

// Revoke executes REVOKE <perm> ON <resource> FROM <role>.
func (c *CassandraDB) Revoke(ctx context.Context, perm Permission, res Resource, role string) error {
	stmt, err := RevokeCQL(perm, res, role)
	if err != nil {
		return err
	}
	return c.Exec(ctx, stmt)
}

// GrantRole executes GRANT <role> TO <grantee> (role hierarchy).
func (c *CassandraDB) GrantRole(ctx context.Context, role, grantee string) error {
	if role == "" || grantee == "" {
		return fmt.Errorf("cassandra: grant role requires role and grantee")
	}
	return c.Exec(ctx, fmt.Sprintf("GRANT %s TO %s", quoteIdent(role), quoteIdent(grantee)))
}

// RevokeRole executes REVOKE <role> FROM <grantee>.
func (c *CassandraDB) RevokeRole(ctx context.Context, role, grantee string) error {
	if role == "" || grantee == "" {
		return fmt.Errorf("cassandra: revoke role requires role and grantee")
	}
	return c.Exec(ctx, fmt.Sprintf("REVOKE %s FROM %s", quoteIdent(role), quoteIdent(grantee)))
}

// ListRolesCQL returns "LIST ROLES" which callers may execute via Query/Iter.
func ListRolesCQL() string { return "LIST ROLES" }

// ListPermissionsCQL returns LIST PERMISSIONS OF <role>.
// If role is empty, returns LIST ALL PERMISSIONS.
func ListPermissionsCQL(role string) string {
	if role == "" {
		return "LIST ALL PERMISSIONS"
	}
	return "LIST ALL PERMISSIONS OF " + quoteIdent(role)
}

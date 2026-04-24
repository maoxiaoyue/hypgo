package cassandra

import (
	"context"
	"fmt"
	"strings"
)

// FunctionArg is a (name, type) argument pair for a UDF / UDA.
type FunctionArg struct {
	Name string
	Type DataType
}

// UDFBuilder builds CREATE / DROP FUNCTION statements.
// Cassandra 5.0 ships with the Java runtime. Other runtimes are not enabled
// by default.
type UDFBuilder struct {
	db            *CassandraDB
	keyspace      string
	name          string
	args          []FunctionArg
	returnType    DataType
	language      string // "java"
	body          string
	orReplace     bool
	ifNotExist    bool
	calledOnNull  bool // default: RETURNS NULL ON NULL INPUT
	deterministic bool
	monotonic     bool
}

// Function returns a UDF builder.
func (c *CassandraDB) Function(name string) *UDFBuilder {
	ks, n := splitQualified(name)
	return &UDFBuilder{db: c, keyspace: ks, name: n, orReplace: true, language: "java"}
}

// IfNotExists toggles IF NOT EXISTS.
func (u *UDFBuilder) IfNotExists(v bool) *UDFBuilder { u.ifNotExist = v; return u }

// OrReplace toggles OR REPLACE (default true).
func (u *UDFBuilder) OrReplace(v bool) *UDFBuilder { u.orReplace = v; return u }

// Arg appends an argument.
func (u *UDFBuilder) Arg(name string, typ DataType) *UDFBuilder {
	u.args = append(u.args, FunctionArg{Name: name, Type: typ})
	return u
}

// Returns sets the return type.
func (u *UDFBuilder) Returns(typ DataType) *UDFBuilder { u.returnType = typ; return u }

// Language sets the UDF language (default "java").
func (u *UDFBuilder) Language(lang string) *UDFBuilder { u.language = lang; return u }

// Body sets the function body source code.
func (u *UDFBuilder) Body(src string) *UDFBuilder { u.body = src; return u }

// CalledOnNullInput toggles CALLED ON NULL INPUT (default is RETURNS NULL ON NULL INPUT).
func (u *UDFBuilder) CalledOnNullInput(v bool) *UDFBuilder { u.calledOnNull = v; return u }

// Deterministic adds the DETERMINISTIC keyword (5.0+).
func (u *UDFBuilder) Deterministic(v bool) *UDFBuilder { u.deterministic = v; return u }

// Monotonic adds the MONOTONIC keyword (5.0+).
func (u *UDFBuilder) Monotonic(v bool) *UDFBuilder { u.monotonic = v; return u }

func (u *UDFBuilder) ref() string {
	if u.keyspace != "" {
		return quoteIdent(u.keyspace) + "." + quoteIdent(u.name)
	}
	return quoteIdent(u.name)
}

func (u *UDFBuilder) argSignature() string {
	parts := make([]string, 0, len(u.args))
	for _, a := range u.args {
		parts = append(parts, quoteIdent(a.Name)+" "+string(a.Type))
	}
	return strings.Join(parts, ", ")
}

// CreateCQL renders CREATE FUNCTION.
func (u *UDFBuilder) CreateCQL() string {
	var sb strings.Builder
	sb.WriteString("CREATE ")
	if u.orReplace {
		sb.WriteString("OR REPLACE ")
	}
	sb.WriteString("FUNCTION ")
	if u.ifNotExist {
		sb.WriteString("IF NOT EXISTS ")
	}
	sb.WriteString(u.ref())
	sb.WriteString("(")
	sb.WriteString(u.argSignature())
	sb.WriteString(")\n")
	if u.calledOnNull {
		sb.WriteString("CALLED ON NULL INPUT\n")
	} else {
		sb.WriteString("RETURNS NULL ON NULL INPUT\n")
	}
	fmt.Fprintf(&sb, "RETURNS %s\n", u.returnType)
	if u.deterministic {
		sb.WriteString("DETERMINISTIC\n")
	}
	if u.monotonic {
		sb.WriteString("MONOTONIC\n")
	}
	lang := u.language
	if lang == "" {
		lang = "java"
	}
	fmt.Fprintf(&sb, "LANGUAGE %s\nAS $$%s$$", lang, u.body)
	return sb.String()
}

// DropCQL renders DROP FUNCTION IF EXISTS.
func (u *UDFBuilder) DropCQL(ifExists bool) string {
	types := make([]string, 0, len(u.args))
	for _, a := range u.args {
		types = append(types, string(a.Type))
	}
	sig := strings.Join(types, ", ")
	if ifExists {
		return fmt.Sprintf("DROP FUNCTION IF EXISTS %s(%s)", u.ref(), sig)
	}
	return fmt.Sprintf("DROP FUNCTION %s(%s)", u.ref(), sig)
}

// Create executes CREATE FUNCTION.
func (u *UDFBuilder) Create(ctx context.Context) error { return u.db.Exec(ctx, u.CreateCQL()) }

// Drop executes DROP FUNCTION IF EXISTS.
func (u *UDFBuilder) Drop(ctx context.Context) error { return u.db.Exec(ctx, u.DropCQL(true)) }

// UDABuilder builds CREATE AGGREGATE statements.
type UDABuilder struct {
	db         *CassandraDB
	keyspace   string
	name       string
	args       []DataType
	sFunc      string
	stateType  DataType
	finalFunc  string
	initCond   string // CQL literal
	orReplace  bool
	ifNotExist bool
}

// Aggregate returns an aggregate builder.
func (c *CassandraDB) Aggregate(name string) *UDABuilder {
	ks, n := splitQualified(name)
	return &UDABuilder{db: c, keyspace: ks, name: n, orReplace: true}
}

// Arg adds an argument type.
func (a *UDABuilder) Arg(typ DataType) *UDABuilder { a.args = append(a.args, typ); return a }

// SFunc sets the state function.
func (a *UDABuilder) SFunc(name string) *UDABuilder { a.sFunc = name; return a }

// StateType sets the stype.
func (a *UDABuilder) StateType(typ DataType) *UDABuilder { a.stateType = typ; return a }

// FinalFunc sets the finalfunc.
func (a *UDABuilder) FinalFunc(name string) *UDABuilder { a.finalFunc = name; return a }

// InitCond sets the INITCOND CQL literal.
func (a *UDABuilder) InitCond(literal string) *UDABuilder { a.initCond = literal; return a }

// IfNotExists toggles IF NOT EXISTS.
func (a *UDABuilder) IfNotExists(v bool) *UDABuilder { a.ifNotExist = v; return a }

// OrReplace toggles OR REPLACE.
func (a *UDABuilder) OrReplace(v bool) *UDABuilder { a.orReplace = v; return a }

func (a *UDABuilder) ref() string {
	if a.keyspace != "" {
		return quoteIdent(a.keyspace) + "." + quoteIdent(a.name)
	}
	return quoteIdent(a.name)
}

// CreateCQL renders CREATE AGGREGATE.
func (a *UDABuilder) CreateCQL() string {
	types := make([]string, 0, len(a.args))
	for _, t := range a.args {
		types = append(types, string(t))
	}
	var sb strings.Builder
	sb.WriteString("CREATE ")
	if a.orReplace {
		sb.WriteString("OR REPLACE ")
	}
	sb.WriteString("AGGREGATE ")
	if a.ifNotExist {
		sb.WriteString("IF NOT EXISTS ")
	}
	fmt.Fprintf(&sb, "%s(%s)\n", a.ref(), strings.Join(types, ", "))
	fmt.Fprintf(&sb, "SFUNC %s\n", a.sFunc)
	fmt.Fprintf(&sb, "STYPE %s", a.stateType)
	if a.finalFunc != "" {
		fmt.Fprintf(&sb, "\nFINALFUNC %s", a.finalFunc)
	}
	if a.initCond != "" {
		fmt.Fprintf(&sb, "\nINITCOND %s", a.initCond)
	}
	return sb.String()
}

// DropCQL renders DROP AGGREGATE IF EXISTS.
func (a *UDABuilder) DropCQL(ifExists bool) string {
	types := make([]string, 0, len(a.args))
	for _, t := range a.args {
		types = append(types, string(t))
	}
	if ifExists {
		return fmt.Sprintf("DROP AGGREGATE IF EXISTS %s(%s)", a.ref(), strings.Join(types, ", "))
	}
	return fmt.Sprintf("DROP AGGREGATE %s(%s)", a.ref(), strings.Join(types, ", "))
}

// Create executes CREATE AGGREGATE.
func (a *UDABuilder) Create(ctx context.Context) error { return a.db.Exec(ctx, a.CreateCQL()) }

// Drop executes DROP AGGREGATE IF EXISTS.
func (a *UDABuilder) Drop(ctx context.Context) error { return a.db.Exec(ctx, a.DropCQL(true)) }

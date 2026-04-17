package cassandra

import (
	"fmt"
	"net"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gocql/gocql"
)

// Model is an optional interface a struct can implement to customise its
// table mapping. Returning values here overrides the defaults inferred from
// struct tags.
type Model interface {
	TableName() string
}

// ModelField represents one column mapped from a Go struct field.
type ModelField struct {
	GoName      string       // Go struct field name
	Name        string       // CQL column name
	Type        DataType     // CQL data type
	Kind        ColumnKind   // partition / clustering / static / regular
	Order       ClusteringOrder
	Position    int          // ordering within partition/clustering keys
	Index       []int        // reflect FieldByIndex path
	OmitEmpty   bool
	Counter     bool
	IsStatic    bool
}

// ModelInfo is the parsed schema of a struct.
type ModelInfo struct {
	Type         reflect.Type
	Keyspace     string
	Table        string
	Fields       []ModelField
	PartitionKey []string // ordered column names
	Clustering   []string // ordered column names
	Options      TableOptions
	// OrderBy mirrors CLUSTERING ORDER BY entries from struct tag hints.
	OrderBy []Column
}

// Columns returns the column names in the registered order.
func (m *ModelInfo) Columns() []string {
	out := make([]string, len(m.Fields))
	for i, f := range m.Fields {
		out[i] = f.Name
	}
	return out
}

// FieldByColumn returns the field mapped to the given CQL column.
func (m *ModelInfo) FieldByColumn(col string) (ModelField, bool) {
	for _, f := range m.Fields {
		if f.Name == col {
			return f, true
		}
	}
	return ModelField{}, false
}

// HasCounter reports whether any column is a counter column.
func (m *ModelInfo) HasCounter() bool {
	for _, f := range m.Fields {
		if f.Counter {
			return true
		}
	}
	return false
}

// modelCache caches parsed schemas keyed by reflect.Type.
var modelCache sync.Map // map[reflect.Type]*ModelInfo

// ParseModel reflects on a struct (or pointer to struct) and extracts its
// Cassandra mapping from `cql:"..."` tags. The tag syntax is:
//
//	cql:"name[,kind][,type=<cql>][,order=asc|desc][,position=N][,omitempty][,static]"
//
// kind  : pk | partition_key | partition | clustering | clustering_key | ck
//
//	        (alias values are accepted)
//	type  : an explicit CQL type expression (e.g. "vector<float, 384>")
//	order : asc | desc (only applies to clustering columns)
//
// A field tagged `cql:"-"` is skipped.
func ParseModel(v interface{}) (*ModelInfo, error) {
	t := reflect.TypeOf(v)
	if t == nil {
		return nil, fmt.Errorf("cassandra: cannot parse nil model")
	}
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("cassandra: model must be a struct, got %s", t.Kind())
	}
	if cached, ok := modelCache.Load(t); ok {
		return cached.(*ModelInfo), nil
	}
	info, err := parseStruct(t)
	if err != nil {
		return nil, err
	}
	if tableNamer, ok := reflect.New(t).Interface().(Model); ok {
		if name := tableNamer.TableName(); name != "" {
			ks, tbl := splitQualified(name)
			if ks != "" {
				info.Keyspace = ks
			}
			info.Table = tbl
		}
	}
	if info.Table == "" {
		info.Table = toSnake(t.Name())
	}
	resolveKeyOrdering(info)
	modelCache.Store(t, info)
	return info, nil
}

func parseStruct(t reflect.Type) (*ModelInfo, error) {
	info := &ModelInfo{Type: t}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("cql")
		if tag == "-" {
			continue
		}
		field, err := parseFieldTag(f, tag)
		if err != nil {
			return nil, fmt.Errorf("cassandra: field %s: %w", f.Name, err)
		}
		field.Index = f.Index
		info.Fields = append(info.Fields, field)
	}
	if len(info.Fields) == 0 {
		return nil, fmt.Errorf("cassandra: no cql fields found in %s", t.Name())
	}
	return info, nil
}

func parseFieldTag(f reflect.StructField, tag string) (ModelField, error) {
	field := ModelField{
		GoName: f.Name,
		Name:   toSnake(f.Name),
	}
	parts := []string{}
	if tag != "" {
		parts = splitTag(tag)
		if n := strings.TrimSpace(parts[0]); n != "" {
			field.Name = n
		}
		parts = parts[1:]
	}
	for _, raw := range parts {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}
		if eq := strings.Index(p, "="); eq >= 0 {
			key := strings.TrimSpace(p[:eq])
			val := strings.TrimSpace(p[eq+1:])
			switch key {
			case "type":
				field.Type = ParseType(val)
			case "order":
				field.Order = parseOrder(val)
			case "position":
				n, err := strconv.Atoi(val)
				if err != nil {
					return field, fmt.Errorf("invalid position: %v", err)
				}
				field.Position = n
			case "name":
				field.Name = val
			default:
				return field, fmt.Errorf("unknown cql tag key %q", key)
			}
			continue
		}
		switch p {
		case "pk", "partition", "partition_key":
			field.Kind = ColumnPartitionKey
		case "ck", "clustering", "clustering_key":
			field.Kind = ColumnClustering
		case "static":
			field.Kind = ColumnStatic
			field.IsStatic = true
		case "counter":
			field.Counter = true
		case "omitempty":
			field.OmitEmpty = true
		default:
			return field, fmt.Errorf("unknown cql tag flag %q", p)
		}
	}
	if field.Type == "" {
		inferred, err := inferType(f.Type, field.Counter)
		if err != nil {
			return field, err
		}
		field.Type = inferred
	}
	return field, nil
}

func parseOrder(v string) ClusteringOrder {
	switch strings.ToLower(v) {
	case "desc", "descending":
		return Desc
	default:
		return Asc
	}
}

type keyEntry struct {
	pos  int
	name string
}

func resolveKeyOrdering(info *ModelInfo) {
	var part, clust []keyEntry
	for _, f := range info.Fields {
		switch f.Kind {
		case ColumnPartitionKey:
			part = append(part, keyEntry{pos: f.Position, name: f.Name})
		case ColumnClustering:
			clust = append(clust, keyEntry{pos: f.Position, name: f.Name})
		}
		if f.Kind == ColumnClustering && f.Order != "" {
			info.OrderBy = append(info.OrderBy, Column{Name: f.Name, Order: f.Order})
		}
	}
	sortByPosition(part)
	sortByPosition(clust)
	info.PartitionKey = make([]string, len(part))
	for i, p := range part {
		info.PartitionKey[i] = p.name
	}
	info.Clustering = make([]string, len(clust))
	for i, c := range clust {
		info.Clustering[i] = c.name
	}
}

// sortByPosition performs a stable sort that only reorders entries with
// explicit non-zero position values. Zero positions preserve their original
// ordering, matching the struct field declaration order.
func sortByPosition(items []keyEntry) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].pos == 0 || items[j].pos == 0 {
			return false
		}
		return items[i].pos < items[j].pos
	})
}

// inferType maps Go types to CQL types. Collections, tuples, UDTs and vectors
// must be declared explicitly via tag `type=...`.
func inferType(t reflect.Type, counter bool) (DataType, error) {
	if counter {
		return TypeCounter, nil
	}
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	// specific named types
	switch t {
	case reflect.TypeOf(time.Time{}):
		return TypeTimestamp, nil
	case reflect.TypeOf(gocql.UUID{}):
		return TypeUUID, nil
	case reflect.TypeOf(net.IP{}):
		return TypeInet, nil
	case reflect.TypeOf(gocql.Duration{}):
		return TypeDuration, nil
	}
	switch t.Kind() {
	case reflect.Bool:
		return TypeBoolean, nil
	case reflect.Int8:
		return TypeTinyInt, nil
	case reflect.Int16:
		return TypeSmallInt, nil
	case reflect.Int, reflect.Int32, reflect.Uint16, reflect.Uint32:
		return TypeInt, nil
	case reflect.Int64, reflect.Uint, reflect.Uint64:
		return TypeBigInt, nil
	case reflect.Float32:
		return TypeFloat, nil
	case reflect.Float64:
		return TypeDouble, nil
	case reflect.String:
		return TypeText, nil
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return TypeBlob, nil
		}
		return "", fmt.Errorf("slice type %s requires explicit cql type tag", t)
	case reflect.Map:
		return "", fmt.Errorf("map type %s requires explicit cql type tag", t)
	case reflect.Struct, reflect.Array:
		return "", fmt.Errorf("type %s requires explicit cql type tag", t)
	}
	return "", fmt.Errorf("unsupported Go type %s", t)
}

// toSnake converts CamelCase to snake_case.
func toSnake(s string) string {
	var sb strings.Builder
	for i, r := range s {
		if i > 0 && isUpper(r) && !isUpper(rune(s[i-1])) {
			sb.WriteByte('_')
		}
		sb.WriteRune(toLower(r))
	}
	return sb.String()
}

// splitTag splits a `cql:"..."` tag body on commas while respecting angle
// brackets, parentheses and square brackets so generic types like
// `vector<float, 384>` or `map<text, int>` are preserved as one segment.
func splitTag(tag string) []string {
	var out []string
	var cur strings.Builder
	depth := 0
	for _, r := range tag {
		switch r {
		case '<', '(', '[':
			depth++
			cur.WriteRune(r)
		case '>', ')', ']':
			if depth > 0 {
				depth--
			}
			cur.WriteRune(r)
		case ',':
			if depth == 0 {
				out = append(out, cur.String())
				cur.Reset()
				continue
			}
			cur.WriteRune(r)
		default:
			cur.WriteRune(r)
		}
	}
	out = append(out, cur.String())
	return out
}

func isUpper(r rune) bool { return r >= 'A' && r <= 'Z' }
func toLower(r rune) rune {
	if isUpper(r) {
		return r + ('a' - 'A')
	}
	return r
}

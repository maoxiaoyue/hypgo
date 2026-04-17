package cassandra

import (
	"fmt"
	"strings"
)

// DataType represents a Cassandra CQL data type.
// Covers native types, collections, tuple, frozen, UDT and Cassandra 5.0 vector type.
type DataType string

// Native CQL data types (Cassandra 5.0 full set).
const (
	TypeAscii     DataType = "ascii"
	TypeBigInt    DataType = "bigint"
	TypeBlob      DataType = "blob"
	TypeBoolean   DataType = "boolean"
	TypeCounter   DataType = "counter"
	TypeDate      DataType = "date"
	TypeDecimal   DataType = "decimal"
	TypeDouble    DataType = "double"
	TypeDuration  DataType = "duration"
	TypeFloat     DataType = "float"
	TypeInet      DataType = "inet"
	TypeInt       DataType = "int"
	TypeSmallInt  DataType = "smallint"
	TypeText      DataType = "text"
	TypeTime      DataType = "time"
	TypeTimestamp DataType = "timestamp"
	TypeTimeUUID  DataType = "timeuuid"
	TypeTinyInt   DataType = "tinyint"
	TypeUUID      DataType = "uuid"
	TypeVarchar   DataType = "varchar"
	TypeVarInt    DataType = "varint"
)

// String returns the CQL representation.
func (t DataType) String() string {
	return string(t)
}

// IsNative reports whether the type is a native CQL primitive (no parameters).
func (t DataType) IsNative() bool {
	switch t {
	case TypeAscii, TypeBigInt, TypeBlob, TypeBoolean, TypeCounter, TypeDate,
		TypeDecimal, TypeDouble, TypeDuration, TypeFloat, TypeInet, TypeInt,
		TypeSmallInt, TypeText, TypeTime, TypeTimestamp, TypeTimeUUID,
		TypeTinyInt, TypeUUID, TypeVarchar, TypeVarInt:
		return true
	}
	return false
}

// List returns a list<T> collection type.
func List(inner DataType) DataType {
	return DataType(fmt.Sprintf("list<%s>", inner))
}

// Set returns a set<T> collection type.
func Set(inner DataType) DataType {
	return DataType(fmt.Sprintf("set<%s>", inner))
}

// Map returns a map<K,V> collection type.
func Map(key, value DataType) DataType {
	return DataType(fmt.Sprintf("map<%s, %s>", key, value))
}

// Tuple returns a tuple<T1,T2,...> type.
func Tuple(types ...DataType) DataType {
	parts := make([]string, len(types))
	for i, t := range types {
		parts[i] = string(t)
	}
	return DataType(fmt.Sprintf("tuple<%s>", strings.Join(parts, ", ")))
}

// Frozen wraps a non-primitive type with frozen<>.
// Frozen collections, tuples and UDTs are stored as a single serialized value.
func Frozen(inner DataType) DataType {
	return DataType(fmt.Sprintf("frozen<%s>", inner))
}

// UDT returns a user-defined type reference.
// Pass a bare name ("address") or a qualified name ("ks.address").
func UDT(name string) DataType {
	return DataType(name)
}

// Vector returns a Cassandra 5.0 vector<T, N> type.
// The element type is usually TypeFloat; dimension must be >= 1.
func Vector(element DataType, dimension int) DataType {
	if dimension < 1 {
		dimension = 1
	}
	return DataType(fmt.Sprintf("vector<%s, %d>", element, dimension))
}

// VectorFloat is a convenience for the common vector<float, N> case.
func VectorFloat(dimension int) DataType {
	return Vector(TypeFloat, dimension)
}

// ParseType normalises a user-supplied type string.
// Accepts any CQL type expression; whitespace around commas is tolerated.
func ParseType(s string) DataType {
	return DataType(strings.TrimSpace(s))
}

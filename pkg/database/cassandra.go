package database

import (
	"fmt"
	"strings"
	"time"

	"github.com/gocql/gocql"
	"github.com/maoxiaoyue/hypgo/pkg/config"
)

type CassandraDB struct {
	config  *config.CassandraConfig
	session *gocql.Session
}

func NewCassandra(cfg *config.CassandraConfig) (*CassandraDB, error) {
	cluster := gocql.NewCluster(cfg.Hosts...)
	cluster.Keyspace = cfg.Keyspace
	cluster.Consistency = gocql.Quorum
	cluster.ProtoVersion = 4
	cluster.ConnectTimeout = time.Second * 10

	session, err := cluster.CreateSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create cassandra session: %w", err)
	}

	return &CassandraDB{
		config:  cfg,
		session: session,
	}, nil
}

func (c *CassandraDB) Session() *gocql.Session {
	return c.session
}

func (c *CassandraDB) Query(query string, args ...interface{}) *gocql.Query {
	return c.session.Query(query, args...)
}

func (c *CassandraDB) Close() {
	if c.session != nil {
		c.session.Close()
	}
}

// CQL Helper Functions
func (c *CassandraDB) CreateTable(tableName string, schema string) error {
	query := fmt.Sprintf(`
        CREATE TABLE IF NOT EXISTS %s (
            %s
        )`, tableName, schema)

	return c.session.Query(query).Exec()
}

func (c *CassandraDB) Insert(table string, data map[string]interface{}) error {
	var columns []string
	var placeholders []string
	var values []interface{}

	for col, val := range data {
		columns = append(columns, col)
		placeholders = append(placeholders, "?")
		values = append(values, val)
	}

	query := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		table,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
	)

	return c.session.Query(query, values...).Exec()
}

func (c *CassandraDB) Select(table string, columns []string, where string, args ...interface{}) *gocql.Iter {
	query := fmt.Sprintf(
		"SELECT %s FROM %s",
		strings.Join(columns, ", "),
		table,
	)

	if where != "" {
		query += " WHERE " + where
	}

	return c.session.Query(query, args...).Iter()
}

func (c *CassandraDB) Update(table string, updates map[string]interface{}, where string, args ...interface{}) error {
	var setClauses []string
	var values []interface{}

	for col, val := range updates {
		setClauses = append(setClauses, col+" = ?")
		values = append(values, val)
	}

	values = append(values, args...)

	query := fmt.Sprintf(
		"UPDATE %s SET %s WHERE %s",
		table,
		strings.Join(setClauses, ", "),
		where,
	)

	return c.session.Query(query, values...).Exec()
}

func (c *CassandraDB) Delete(table string, where string, args ...interface{}) error {
	query := fmt.Sprintf("DELETE FROM %s WHERE %s", table, where)
	return c.session.Query(query, args...).Exec()
}

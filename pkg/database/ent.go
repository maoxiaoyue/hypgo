package database

import (
	"context"
	"database/sql"
	"fmt"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	"github.com/maoxiaoyue/hypgo/pkg/config"
	"github.com/redis/go-redis/v9"
)

type Database struct {
	config    *config.DatabaseConfig
	sqlDB     *sql.DB
	entDriver *entsql.Driver
	redisDB   *redis.Client
}

func New(cfg *config.DatabaseConfig) (*Database, error) {
	db := &Database{config: cfg}

	switch cfg.Driver {
	case "mysql", "tidb":
		return db.initMySQL()
	case "postgres":
		return db.initPostgres()
	case "redis":
		return db.initRedis()
	default:
		return nil, fmt.Errorf("unsupported database driver: %s", cfg.Driver)
	}
}

func (d *Database) initMySQL() (*Database, error) {
	db, err := sql.Open("mysql", d.config.DSN)
	if err != nil {
		return nil, fmt.Errorf("failed to open mysql: %w", err)
	}

	db.SetMaxIdleConns(d.config.MaxIdleConns)
	db.SetMaxOpenConns(d.config.MaxOpenConns)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping mysql: %w", err)
	}

	d.sqlDB = db
	d.entDriver = entsql.OpenDB(dialect.MySQL, db)

	return d, nil
}

func (d *Database) initPostgres() (*Database, error) {
	db, err := sql.Open("postgres", d.config.DSN)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres: %w", err)
	}

	db.SetMaxIdleConns(d.config.MaxIdleConns)
	db.SetMaxOpenConns(d.config.MaxOpenConns)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	d.sqlDB = db
	d.entDriver = entsql.OpenDB(dialect.Postgres, db)

	return d, nil
}

func (d *Database) initRedis() (*Database, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     d.config.Redis.Addr,
		Password: d.config.Redis.Password,
		DB:       d.config.Redis.DB,
	})

	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("failed to ping redis: %w", err)
	}

	d.redisDB = client
	return d, nil
}

func (d *Database) EntDriver() *entsql.Driver {
	return d.entDriver
}

func (d *Database) Redis() *redis.Client {
	return d.redisDB
}

func (d *Database) Close() error {
	if d.sqlDB != nil {
		return d.sqlDB.Close()
	}
	if d.redisDB != nil {
		return d.redisDB.Close()
	}
	return nil
}

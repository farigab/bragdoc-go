// Package db provides helpers for building SQL queries and a DB adapter.
// It contains query builders, argument escaping utilities and a small DB
// wrapper used throughout the application.
package db

import (
	"context"
	"fmt"

	sqlitecloud "github.com/sqlitecloud/sqlitecloud-go"
)

// Queryable é implementado por qualquer builder que produz SQL.
type Queryable interface {
	Build() (string, error)
}

// DB é a abstração de banco de dados da aplicação.
// Query/Exec aceitam builders; QueryRaw/ExecRaw aceitam SQL pré-montado
// (migrações, queries complexas).
type DB interface {
	Query(ctx context.Context, q Queryable) (*Rows, error)
	Exec(ctx context.Context, q Queryable) error
	QueryRaw(ctx context.Context, sql string) (*Rows, error)
	ExecRaw(ctx context.Context, sql string) error
}

type sqCloudDB struct {
	conn *sqlitecloud.SQCloud
}

// New envolve uma conexão SQCloud na interface DB da aplicação.
func New(conn *sqlitecloud.SQCloud) DB {
	return &sqCloudDB{conn: conn}
}

func (d *sqCloudDB) Query(ctx context.Context, q Queryable) (*Rows, error) {
	sql, err := q.Build()
	if err != nil {
		return nil, fmt.Errorf("db: build query: %w", err)
	}
	return d.QueryRaw(ctx, sql)
}

func (d *sqCloudDB) Exec(ctx context.Context, q Queryable) error {
	sql, err := q.Build()
	if err != nil {
		return fmt.Errorf("db: build query: %w", err)
	}
	return d.ExecRaw(ctx, sql)
}

func (d *sqCloudDB) QueryRaw(ctx context.Context, sql string) (*Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result, err := d.conn.Select(sql)
	if err != nil {
		return nil, err
	}
	return &Rows{r: result}, nil
}

func (d *sqCloudDB) ExecRaw(ctx context.Context, sql string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.conn.Execute(sql)
}

package db

import (
	"time"

	sqlitecloud "github.com/sqlitecloud/sqlitecloud-go"
)

// Rows é um wrapper tipado sobre *sqlitecloud.Result.
// Todos os acessores são zero-value-safe — coluna ausente retorna zero value.
type Rows struct {
	r *sqlitecloud.Result
}

// Len returns the number of rows in the result. It is zero-safe.
func (rs *Rows) Len() int {
	if rs == nil || rs.r == nil {
		return 0
	}
	return int(rs.r.GetNumberOfRows())
}

// IsEmpty reports whether the result contains no rows.
func (rs *Rows) IsEmpty() bool { return rs.Len() == 0 }

// String returns the string value at the specified row and column.
// If the underlying value is absent it returns the zero value (empty string).
func (rs *Rows) String(row, col int) string {
	v, _ := rs.r.GetStringValue(uint64(row), uint64(col))
	return v
}

// Int64 returns the int64 value at the specified row and column.
// Absent or invalid values yield the zero value.
func (rs *Rows) Int64(row, col int) int64 {
	v, _ := rs.r.GetInt64Value(uint64(row), uint64(col))
	return v
}

// Bool lê uma coluna inteira e retorna true quando != 0.
func (rs *Rows) Bool(row, col int) bool { return rs.Int64(row, col) != 0 }

// Time parseia uma coluna string com TimeLayout e retorna UTC.
// Retorna zero time em caso de falha de parse.
func (rs *Rows) Time(row, col int) time.Time {
	s := rs.String(row, col)
	if s == "" {
		return time.Time{}
	}
	t, _ := time.ParseInLocation(TimeLayout, s, time.UTC)
	return t
}

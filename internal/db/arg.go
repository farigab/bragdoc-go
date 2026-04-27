package db

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// TimeLayout é o formato que o datetime() do SQLite entende.
// RFC3339 usa 'T'/'Z' que o SQLite não reconhece — nunca mude isso.
const TimeLayout = "2006-01-02 15:04:05"

// Arg é um valor SQL tipado e escapado.
// Construa apenas via String, Int64, Bool, Time, Null, RawSQL.
// O campo err permite propagar falhas de validação até o Build(),
// sem panic e sem retorno múltiplo nos call sites.
type Arg struct {
	literal string
	err     error
}

// String escapa s de forma segura para uso como literal SQL.
func String(v string) Arg {
	if strings.ContainsRune(v, 0) {
		return Arg{err: fmt.Errorf("db: null byte not allowed in value")}
	}
	return Arg{literal: "'" + strings.ReplaceAll(v, "'", "''") + "'"}
}

// Int64 returns an Arg representing the integer value v.
func Int64(v int64) Arg { return Arg{literal: strconv.FormatInt(v, 10)} }

// Bool returns an Arg representing the boolean value v as an integer
// literal (1 for true, 0 for false).
func Bool(v bool) Arg {
	if v {
		return Arg{literal: "1"}
	}
	return Arg{literal: "0"}
}

// Time formata em UTC com o layout que o SQLite entende.
func Time(v time.Time) Arg {
	return Arg{literal: "'" + v.UTC().Format(TimeLayout) + "'"}
}

// Null returns an Arg representing SQL NULL.
func Null() Arg { return Arg{literal: "NULL"} }

// RawSQL injeta uma expressão SQL sem escape.
// Use SOMENTE para expressões estáticas confiáveis: datetime('now'), excluded.col, etc.
func RawSQL(expr string) Arg { return Arg{literal: expr} }

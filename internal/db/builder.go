package db

import (
	"fmt"
	"strings"
)

const wherePrefix = " WHERE "

// ─── Condition ───────────────────────────────────────────────────────────────

// Condition representa um predicado de WHERE clause.
type Condition struct {
	expr string
	err  error
}

// Eq builds an equality WHERE condition for column col and value val.
func Eq(col string, val Arg) Condition {
	if val.err != nil {
		return Condition{err: val.err}
	}
	return Condition{expr: col + " = " + val.literal}
}

// Lt builds a less-than WHERE condition for column col and value val.
func Lt(col string, val Arg) Condition {
	if val.err != nil {
		return Condition{err: val.err}
	}
	return Condition{expr: col + " < " + val.literal}
}

// RawCond injeta um predicado SQL estático confiável.
// Exemplo: RawCond("expires_at < datetime('now')")
func RawCond(expr string) Condition { return Condition{expr: expr} }

// ─── Assignment ──────────────────────────────────────────────────────────────

// Assignment representa uma atribuição em ON CONFLICT DO UPDATE SET.
type Assignment struct{ expr string }

// Set gera "col = val".
func Set(col string, val Arg) Assignment {
	return Assignment{expr: col + " = " + val.literal}
}

// SetExcluded gera "col = excluded.col".
func SetExcluded(col string) Assignment {
	return Assignment{expr: col + " = excluded." + col}
}

// SetRaw gera "col = <expr>" sem escape.
// Use para CASE WHEN e expressões que referenciam colunas.
func SetRaw(col, expr string) Assignment {
	return Assignment{expr: col + " = " + expr}
}

// ─── SELECT ──────────────────────────────────────────────────────────────────

// SelectBuilder builds SQL SELECT queries.
type SelectBuilder struct {
	cols   []string
	table  string
	wheres []Condition
	limit  int
}

// Select creates a new SelectBuilder for the provided columns.
func Select(cols ...string) *SelectBuilder { return &SelectBuilder{cols: cols} }

// From sets the table for the SELECT builder.
func (b *SelectBuilder) From(table string) *SelectBuilder { b.table = table; return b }

// Limit sets a LIMIT clause on the SELECT.
func (b *SelectBuilder) Limit(n int) *SelectBuilder { b.limit = n; return b }

// Where appends WHERE conditions to the SELECT.
func (b *SelectBuilder) Where(c ...Condition) *SelectBuilder {
	b.wheres = append(b.wheres, c...)
	return b
}

// Build constructs the SELECT SQL string from the builder.
func (b *SelectBuilder) Build() (string, error) {
	if b.table == "" {
		return "", fmt.Errorf("db: SELECT requires FROM table")
	}
	cols := "*"
	if len(b.cols) > 0 {
		cols = strings.Join(b.cols, ", ")
	}
	q := fmt.Sprintf("SELECT %s FROM %s", cols, b.table)
	if where, err := buildWhere(b.wheres); err != nil {
		return "", err
	} else if where != "" {
		q += wherePrefix + where
	}
	if b.limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", b.limit)
	}
	return q + ";", nil
}

// ─── INSERT ──────────────────────────────────────────────────────────────────

// InsertBuilder builds SQL INSERT queries.
type InsertBuilder struct {
	table       string
	cols        []string
	vals        []Arg
	conflictCol string
	doUpdate    []Assignment
	returning   []string
}

// Insert creates a new InsertBuilder for the given table.
func Insert(table string) *InsertBuilder { return &InsertBuilder{table: table} }

// Columns sets the columns for the INSERT.
func (b *InsertBuilder) Columns(cols ...string) *InsertBuilder { b.cols = cols; return b }

// Values sets the values for the INSERT.
func (b *InsertBuilder) Values(vals ...Arg) *InsertBuilder { b.vals = vals; return b }

// OnConflict sets the conflict target column for UPSERT behavior.
func (b *InsertBuilder) OnConflict(col string) *InsertBuilder { b.conflictCol = col; return b }

// Returning sets the RETURNING clause for the INSERT.
func (b *InsertBuilder) Returning(cols ...string) *InsertBuilder { b.returning = cols; return b }

// DoUpdate adds assignments for ON CONFLICT DO UPDATE.
func (b *InsertBuilder) DoUpdate(assignments ...Assignment) *InsertBuilder {
	b.doUpdate = append(b.doUpdate, assignments...)
	return b
}

// Build constructs the INSERT SQL string from the builder.
func (b *InsertBuilder) Build() (string, error) {
	if b.table == "" {
		return "", fmt.Errorf("db: INSERT requires a table")
	}
	if len(b.cols) != len(b.vals) {
		return "", fmt.Errorf("db: INSERT col/val mismatch: %d cols, %d vals", len(b.cols), len(b.vals))
	}

	literals := make([]string, len(b.vals))
	for i, v := range b.vals {
		if v.err != nil {
			return "", fmt.Errorf("db: INSERT values[%d]: %w", i, v.err)
		}
		literals[i] = v.literal
	}

	q := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		b.table,
		strings.Join(b.cols, ", "),
		strings.Join(literals, ", "),
	)

	if b.conflictCol != "" {
		if len(b.doUpdate) > 0 {
			sets := make([]string, len(b.doUpdate))
			for i, a := range b.doUpdate {
				sets[i] = a.expr
			}
			q += fmt.Sprintf(" ON CONFLICT(%s) DO UPDATE SET %s",
				b.conflictCol, strings.Join(sets, ", "))
		} else {
			q += fmt.Sprintf(" ON CONFLICT(%s) DO NOTHING", b.conflictCol)
		}
	}

	if len(b.returning) > 0 {
		q += " RETURNING " + strings.Join(b.returning, ", ")
	}
	return q + ";", nil
}

// ─── UPDATE ──────────────────────────────────────────────────────────────────

// UpdateBuilder builds SQL UPDATE queries.
type UpdateBuilder struct {
	table  string
	sets   []string
	errs   []error
	wheres []Condition
}

// Update creates a new UpdateBuilder for the specified table.
func Update(table string) *UpdateBuilder { return &UpdateBuilder{table: table} }

// Where appends WHERE conditions to the UPDATE.
func (b *UpdateBuilder) Where(c ...Condition) *UpdateBuilder {
	b.wheres = append(b.wheres, c...)
	return b
}

// Set adds a column assignment to the UPDATE using the provided Arg value.
func (b *UpdateBuilder) Set(col string, val Arg) *UpdateBuilder {
	if val.err != nil {
		b.errs = append(b.errs, fmt.Errorf("db: SET %s: %w", col, val.err))
	} else {
		b.sets = append(b.sets, col+" = "+val.literal)
	}
	return b
}

// Build constructs the UPDATE SQL string from the builder.
func (b *UpdateBuilder) Build() (string, error) {
	for _, e := range b.errs {
		if e != nil {
			return "", e
		}
	}
	if b.table == "" || len(b.sets) == 0 {
		return "", fmt.Errorf("db: UPDATE requires table and at least one SET")
	}
	q := fmt.Sprintf("UPDATE %s SET %s", b.table, strings.Join(b.sets, ", "))
	if where, err := buildWhere(b.wheres); err != nil {
		return "", err
	} else if where != "" {
		q += wherePrefix + where
	}
	return q + ";", nil
}

// ─── DELETE ──────────────────────────────────────────────────────────────────

// DeleteBuilder builds SQL DELETE queries.
type DeleteBuilder struct {
	table  string
	wheres []Condition
}

// Delete creates a new DeleteBuilder for the specified table.
func Delete(table string) *DeleteBuilder { return &DeleteBuilder{table: table} }

// Where appends WHERE conditions to the DELETE.
func (b *DeleteBuilder) Where(c ...Condition) *DeleteBuilder {
	b.wheres = append(b.wheres, c...)
	return b
}

// Build constructs the DELETE SQL string from the builder.
func (b *DeleteBuilder) Build() (string, error) {
	if b.table == "" {
		return "", fmt.Errorf("db: DELETE requires a table")
	}
	q := "DELETE FROM " + b.table
	if where, err := buildWhere(b.wheres); err != nil {
		return "", err
	} else if where != "" {
		q += wherePrefix + where
	}
	return q + ";", nil
}

// ─── helpers internos ────────────────────────────────────────────────────────

func buildWhere(conds []Condition) (string, error) {
	if len(conds) == 0 {
		return "", nil
	}
	parts := make([]string, len(conds))
	for i, c := range conds {
		if c.err != nil {
			return "", fmt.Errorf("db: WHERE[%d]: %w", i, c.err)
		}
		parts[i] = c.expr
	}
	return strings.Join(parts, " AND "), nil
}

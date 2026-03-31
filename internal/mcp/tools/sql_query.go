package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/lib/pq"

	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
)

const sqlQueryTimeout = 10 * time.Second

// SQLQueryTool executes read-only SQL queries.
type SQLQueryTool struct {
	dsn string
}

func NewSQLQueryTool(dsn string) *SQLQueryTool {
	return &SQLQueryTool{dsn: dsn}
}

func (t *SQLQueryTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "sql_query",
		Description: "Execute a read-only SQL query against the database.",
		InputSchema: types.ToolSchema{
			Type: "object",
			Properties: map[string]types.Property{
				"sql": {Type: "string", Description: "SQL SELECT query"},
			},
			Required: []string{"sql"},
		},
		RateLimit: 20,
		Category:  "data",
	}
}

func (t *SQLQueryTool) Handle(ctx context.Context, input map[string]any) (string, error) {
	statement, ok := input["sql"].(string)
	if !ok || strings.TrimSpace(statement) == "" {
		return "", fmt.Errorf("sql must be a non-empty string")
	}
	if !isSelectQuery(statement) {
		return "", fmt.Errorf("only SELECT queries are allowed")
	}
	if strings.TrimSpace(t.dsn) == "" {
		return "", fmt.Errorf("sql_query dsn is not configured")
	}

	db, err := sql.Open("postgres", t.dsn)
	if err != nil {
		return "", fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	queryCtx, cancel := context.WithTimeout(ctx, sqlQueryTimeout)
	defer cancel()

	tx, err := db.BeginTx(queryCtx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return "", fmt.Errorf("begin read-only transaction: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(queryCtx, statement)
	if err != nil {
		return "", fmt.Errorf("query database: %w", err)
	}
	defer rows.Close()

	records, err := scanRows(rows)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit read-only transaction: %w", err)
	}

	payload, err := json.Marshal(records)
	if err != nil {
		return "", fmt.Errorf("marshal query result: %w", err)
	}
	return string(payload), nil
}

func isSelectQuery(statement string) bool {
	normalized := strings.TrimSpace(strings.ToUpper(statement))
	return strings.HasPrefix(normalized, "SELECT")
}

func scanRows(rows *sql.Rows) ([]map[string]any, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("load columns: %w", err)
	}

	records := make([]map[string]any, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		scanTargets := make([]any, len(columns))
		for idx := range values {
			scanTargets[idx] = &values[idx]
		}
		if err := rows.Scan(scanTargets...); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}

		record := make(map[string]any, len(columns))
		for idx, column := range columns {
			record[column] = normalizeSQLValue(values[idx])
		}
		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}
	return records, nil
}

func normalizeSQLValue(value any) any {
	if bytesValue, ok := value.([]byte); ok {
		return string(bytesValue)
	}
	return value
}

package sqlitestore

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/conductor-ai/conductor/core/ports"
)

// rowScanner is satisfied by both *sql.Row and *sql.Rows, letting one scan
// helper serve single-row and multi-row queries.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanTrace reads one trace row (column order must match the SELECTs above).
func scanTrace(sc rowScanner) (ports.Trace, error) {
	var (
		t            ports.Trace
		stream       int
		finalProv    sql.NullString
		errStr       sql.NullString
		finalStatus  string
		attemptsJSON string
	)
	err := sc.Scan(
		&t.ID, &t.CreatedUnix, &t.RequestModel, &t.MessageCount, &stream,
		&finalProv, &finalStatus, &errStr,
		&t.Usage.PromptTokens, &t.Usage.CompletionTokens, &t.Usage.TotalTokens,
		&t.CostUSD, &t.LatencyMS, &attemptsJSON,
	)
	if err != nil {
		return ports.Trace{}, err
	}
	t.Stream = stream != 0
	t.FinalProvider = finalProv.String
	t.FinalStatus = ports.AttemptStatus(finalStatus)
	t.Error = errStr.String
	if attemptsJSON != "" {
		if err := json.Unmarshal([]byte(attemptsJSON), &t.Attempts); err != nil {
			return ports.Trace{}, err
		}
	}
	return t, nil
}

// scanRun reads one workflow-run row (column order must match the SELECTs above).
func scanRun(sc rowScanner) (ports.WorkflowRun, error) {
	var (
		r         ports.WorkflowRun
		status    string
		trigger   sql.NullString
		errStr    sql.NullString
		nodesJSON string
	)
	err := sc.Scan(
		&r.ID, &r.Workflow, &r.CreatedUnix, &status, &trigger, &errStr,
		&r.Usage.PromptTokens, &r.Usage.CompletionTokens, &r.Usage.TotalTokens,
		&r.CostUSD, &r.LatencyMS, &nodesJSON,
	)
	if err != nil {
		return ports.WorkflowRun{}, err
	}
	r.Status = ports.RunStatus(status)
	r.Trigger = trigger.String
	r.Error = errStr.String
	if nodesJSON != "" {
		if err := json.Unmarshal([]byte(nodesJSON), &r.Nodes); err != nil {
			return ports.WorkflowRun{}, err
		}
	}
	return r, nil
}

// scanPrompt reads one prompt row.
func scanPrompt(sc rowScanner) (ports.Prompt, error) {
	var (
		p    ports.Prompt
		desc sql.NullString
	)
	if err := sc.Scan(&p.Name, &p.Version, &p.Template, &desc, &p.CreatedUnix); err != nil {
		return ports.Prompt{}, err
	}
	p.Description = desc.String
	return p, nil
}

// --- tiny value helpers --------------------------------------------------

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// nullStr maps "" to a SQL NULL so empty optional fields don't store as "".
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nowUnix() int64 { return time.Now().Unix() }

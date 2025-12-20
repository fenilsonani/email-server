// Package sieve implements RFC 5228 Sieve email filtering language
package sieve

import (
	"context"
	"database/sql"
	"time"
)

// Script represents a stored Sieve script
type Script struct {
	ID        int64
	UserID    int64
	Name      string
	Content   string
	IsActive  bool
	CreatedAt time.Time
	UpdatedAt time.Time
	Parsed    *ParsedScript // Compiled representation, nil if not parsed
}

// ParsedScript is the compiled representation of a Sieve script
type ParsedScript struct {
	Require []string
	Rules   []Rule
}

// Rule represents a single if/elsif/else block in Sieve
type Rule struct {
	Conditions []Condition
	Actions    []Action
	AllOf      bool // true = allof (AND), false = anyof (OR)
	Stop       bool // stop processing after this rule
}

// Result represents the outcome of Sieve script execution
type Result struct {
	Keep       bool     // Default action - deliver to INBOX
	Discarded  bool     // Message should be discarded
	Rejected   bool     // Message should be rejected
	RejectMsg  string   // Rejection message
	Filed      bool     // Message should be filed to folder
	FileInto   string   // Target folder name
	Redirected bool     // Message should be redirected
	RedirectTo []string // Redirect addresses
	Vacation   bool     // Vacation response should be sent
	VacationTo string   // Vacation response recipient
	VacationSubject string
	VacationBody    string
}

// Message represents an email message for Sieve evaluation
type Message struct {
	From        string
	To          []string
	Subject     string
	Headers     map[string][]string
	Size        int64
	Body        []byte
	Date        time.Time
	InternalDate time.Time
}

// Executor executes Sieve scripts against messages
type Executor struct {
	store         *Store
	vacationStore *VacationStore
	db            *sql.DB
}

// NewExecutor creates a new Sieve executor
func NewExecutor(db *sql.DB) *Executor {
	return &Executor{
		store:         NewStore(db),
		vacationStore: NewVacationStore(db),
		db:            db,
	}
}

// Execute runs the active Sieve script for a user against a message
func (e *Executor) Execute(ctx context.Context, userID int64, msg *Message) (*Result, error) {
	// Get active script for user
	script, err := e.store.GetActiveScript(ctx, userID)
	if err != nil {
		return nil, err
	}

	// No active script, use default (keep)
	if script == nil {
		return &Result{Keep: true}, nil
	}

	// Parse script if not already parsed
	if script.Parsed == nil {
		parsed, err := Parse(script.Content)
		if err != nil {
			// Script parse error - fall back to keep
			return &Result{Keep: true}, nil
		}
		script.Parsed = parsed
	}

	// Execute the script
	return e.executeScript(ctx, userID, script.Parsed, msg)
}

// executeScript runs a parsed script against a message
func (e *Executor) executeScript(ctx context.Context, userID int64, script *ParsedScript, msg *Message) (*Result, error) {
	result := &Result{Keep: true} // Default action

	for _, rule := range script.Rules {
		// Check if rule conditions match
		matched := e.evaluateConditions(rule.Conditions, rule.AllOf, msg)

		if matched {
			// Execute actions
			for _, action := range rule.Actions {
				if err := action.Apply(ctx, result, msg, e.vacationStore, userID); err != nil {
					// Log error but continue
					continue
				}
			}

			// If stop is set or explicit action taken, don't process more rules
			if rule.Stop || result.Discarded || result.Rejected || result.Filed || result.Redirected {
				break
			}
		}
	}

	return result, nil
}

// evaluateConditions checks if message matches rule conditions
func (e *Executor) evaluateConditions(conditions []Condition, allOf bool, msg *Message) bool {
	if len(conditions) == 0 {
		return true
	}

	for _, cond := range conditions {
		matches := cond.Evaluate(msg)

		if allOf && !matches {
			return false // AND - all must match
		}
		if !allOf && matches {
			return true // OR - any match is enough
		}
	}

	// For allOf, reaching here means all matched
	// For anyof, reaching here means none matched
	return allOf
}

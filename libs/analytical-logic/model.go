package analyticallogic

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// AnalyticalExpression is a saved analytical expression (Foundry
// "visual function template").
//
// Payload is intentionally json.RawMessage because the Foundry visual
// function templates surface stores arbitrary JSON (display name,
// parameters, body, dependencies, …); validating the shape is the
// consumer's responsibility.
type AnalyticalExpression struct {
	ID        uuid.UUID       `json:"id"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// AnalyticalExpressionVersion is one historical version of an
// AnalyticalExpression.
type AnalyticalExpressionVersion struct {
	ID        uuid.UUID       `json:"id"`
	ParentID  uuid.UUID       `json:"parent_id"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

// NewExpression is the insert payload accepted by
// AnalyticalExpressionRepo.Create.
type NewExpression struct {
	Payload json.RawMessage `json:"payload"`
}

// NewExpressionVersion is the insert payload accepted by
// AnalyticalExpressionRepo.AddVersion.
type NewExpressionVersion struct {
	Payload json.RawMessage `json:"payload"`
}

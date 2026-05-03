// Package workflows hosts the OntologyReindex workflow. Substrate:
// the workflow body is a deterministic loop that pages Cassandra
// (via activity) and publishes batches to the reindex topic.
package workflows

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/open-foundry/open-foundry/workers-go/reindex/internal/contract"
)

// OntologyReindex iterates over a tenant's objects in Cassandra
// and publishes each batch to `ontology.reindex.v1`. The workflow
// is deterministic: the page token chain reproduces identically on
// replay, and the publish activity is idempotent because every
// record carries a deterministic `event_id` (UUID v5 of
// `aggregate || aggregate_id || version`).
//
// Backoff and retries are handled by Temporal's activity options,
// not by application loops, so a transient Cassandra/Kafka outage
// resumes from the failed page rather than restarting the run.
func OntologyReindex(ctx workflow.Context, input contract.OntologyReindexInput) (*contract.OntologyReindexResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("OntologyReindex started", "tenant_id", input.TenantID, "type_id", input.TypeID)

	if input.PageSize == 0 {
		input.PageSize = 1000
	}

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		HeartbeatTimeout:    30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute,
			MaximumAttempts:    0, // unlimited; backoff is the rate-limiter
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	res := &contract.OntologyReindexResult{TenantID: input.TenantID, Status: "completed"}
	resumeToken := input.ResumeToken

	for {
		var page scanPage
		err := workflow.ExecuteActivity(ctx, contract.ActivityScanCassandra, scanInput{
			TenantID:    input.TenantID,
			TypeID:      input.TypeID,
			PageSize:    input.PageSize,
			ResumeToken: resumeToken,
		}).Get(ctx, &page)
		if err != nil {
			res.Status = "failed"
			res.Error = err.Error()
			return res, err
		}

		if len(page.Records) == 0 {
			break
		}
		res.Scanned += int64(len(page.Records))

		var pubResult publishResult
		err = workflow.ExecuteActivity(ctx, contract.ActivityPublishReindexBatch, publishInput{
			Topic:   contract.TopicReindex,
			Records: page.Records,
		}).Get(ctx, &pubResult)
		if err != nil {
			res.Status = "failed"
			res.Error = err.Error()
			return res, err
		}
		res.Published += pubResult.Published

		if page.NextToken == "" {
			break
		}
		resumeToken = page.NextToken

		// Continue-as-new every 50k objects to keep the workflow
		// history bounded.
		if res.Scanned%50000 == 0 {
			next := input
			next.ResumeToken = resumeToken
			return nil, workflow.NewContinueAsNewError(ctx, OntologyReindex, next)
		}
	}

	logger.Info("OntologyReindex done", "scanned", res.Scanned, "published", res.Published)
	return res, nil
}

// scanInput / scanPage / publishInput / publishResult are the
// activity payloads exchanged with the worker activities.

type scanInput struct {
	TenantID    string `json:"tenant_id"`
	TypeID      string `json:"type_id,omitempty"`
	PageSize    int    `json:"page_size"`
	ResumeToken string `json:"resume_token,omitempty"`
}

type scanPage struct {
	Records   []map[string]any `json:"records"`
	NextToken string           `json:"next_token,omitempty"`
}

type publishInput struct {
	Topic   string           `json:"topic"`
	Records []map[string]any `json:"records"`
}

type publishResult struct {
	Published int64 `json:"published"`
}

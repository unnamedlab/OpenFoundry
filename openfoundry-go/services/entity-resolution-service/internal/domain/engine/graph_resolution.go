package engine

import (
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/models"
)

// GraphResolutionResult mirrors fusion_base::domain::engine::graph_resolution::GraphResolutionResult.
type GraphResolutionResult struct {
	Clusters    []models.ResolvedCluster
	ReviewItems []models.ReviewQueueItem
}

// ResolveClusters ports `graph_resolution::resolve_clusters` exactly:
// union-find over records linked by evidences whose final_score >=
// review_threshold, then cluster bookkeeping + review item synthesis.
func ResolveClusters(jobID uuid.UUID, records []models.EntityRecord, evidences []models.MatchEvidence, reviewThreshold, autoMergeThreshold float32) GraphResolutionResult {
	uf := newUnionFind(len(records))
	positions := make(map[string]int, len(records))
	for i, record := range records {
		positions[record.RecordID] = i
	}

	for _, evidence := range evidences {
		if evidence.FinalScore < reviewThreshold {
			continue
		}
		li, lok := positions[evidence.LeftRecordID]
		ri, rok := positions[evidence.RightRecordID]
		if !lok || !rok {
			continue
		}
		uf.union(li, ri)
	}

	groups := map[int][]models.EntityRecord{}
	for i, record := range records {
		root := uf.find(i)
		groups[root] = append(groups[root], record)
	}
	rootKeys := make([]int, 0, len(groups))
	for k := range groups {
		rootKeys = append(rootKeys, k)
	}
	sort.Ints(rootKeys)

	clusters := []models.ResolvedCluster{}
	reviewItems := []models.ReviewQueueItem{}
	now := time.Now().UTC()

	for _, root := range rootKeys {
		clusterRecords := groups[root]
		clusterID := MustNewUUIDv7()
		idSet := map[string]struct{}{}
		for _, r := range clusterRecords {
			idSet[r.RecordID] = struct{}{}
		}

		clusterEvidence := []models.MatchEvidence{}
		for _, evidence := range evidences {
			_, lok := idSet[evidence.LeftRecordID]
			_, rok := idSet[evidence.RightRecordID]
			if lok && rok && evidence.FinalScore >= reviewThreshold {
				clusterEvidence = append(clusterEvidence, evidence)
			}
		}

		requiresReview := false
		for _, evidence := range clusterEvidence {
			if evidence.FinalScore >= reviewThreshold && evidence.FinalScore < autoMergeThreshold {
				requiresReview = true
				break
			}
		}

		var confidenceScore float32
		if len(clusterEvidence) == 0 {
			total := float32(0)
			for _, r := range clusterRecords {
				total += r.Confidence
			}
			denom := len(clusterRecords)
			if denom < 1 {
				denom = 1
			}
			confidenceScore = clamp01(total / float32(denom))
		} else {
			total := float32(0)
			for _, evidence := range clusterEvidence {
				total += evidence.FinalScore
			}
			denom := len(clusterEvidence)
			if denom < 1 {
				denom = 1
			}
			confidenceScore = clamp01(total / float32(denom))
		}

		var status string
		switch {
		case len(clusterEvidence) == 0:
			status = "singleton"
		case requiresReview:
			status = "pending_review"
		default:
			status = "resolved"
		}

		recordIDs := make([]string, 0, len(clusterRecords))
		for _, r := range clusterRecords {
			recordIDs = append(recordIDs, r.RecordID)
		}
		clusterKey := strings.Join(recordIDs, "|")

		cluster := models.ResolvedCluster{
			ID:                      clusterID,
			JobID:                   jobID,
			ClusterKey:              clusterKey,
			Status:                  status,
			Records:                 append([]models.EntityRecord(nil), clusterRecords...),
			Evidence:                append([]models.MatchEvidence(nil), clusterEvidence...),
			ConfidenceScore:         confidenceScore,
			RequiresReview:          requiresReview,
			SuggestedGoldenRecordID: nil,
			CreatedAt:               now,
			UpdatedAt:               now,
		}

		if requiresReview {
			rationale := []string{}
			for i, evidence := range clusterEvidence {
				if i >= 3 {
					break
				}
				rationale = append(rationale, evidence.Explanation)
			}
			severity := "medium"
			if confidenceScore < autoMergeThreshold {
				severity = "high"
			}
			reviewItems = append(reviewItems, models.ReviewQueueItem{
				ID:                MustNewUUIDv7(),
				ClusterID:         clusterID,
				Status:            "pending",
				Severity:          severity,
				RecommendedAction: "manual_review",
				Rationale:         rationale,
				AssignedTo:        nil,
				ReviewedBy:        nil,
				Notes:             "",
				CreatedAt:         now,
				UpdatedAt:         now,
			})
		}

		clusters = append(clusters, cluster)
	}

	sort.SliceStable(clusters, func(i, j int) bool {
		return len(clusters[i].Records) > len(clusters[j].Records)
	})

	return GraphResolutionResult{Clusters: clusters, ReviewItems: reviewItems}
}

type unionFind struct {
	parents []int
	ranks   []int
}

func newUnionFind(size int) *unionFind {
	parents := make([]int, size)
	for i := range parents {
		parents[i] = i
	}
	return &unionFind{parents: parents, ranks: make([]int, size)}
}

func (u *unionFind) find(index int) int {
	if u.parents[index] != index {
		u.parents[index] = u.find(u.parents[index])
	}
	return u.parents[index]
}

func (u *unionFind) union(left, right int) {
	lr := u.find(left)
	rr := u.find(right)
	if lr == rr {
		return
	}
	switch {
	case u.ranks[lr] < u.ranks[rr]:
		u.parents[lr] = rr
	case u.ranks[lr] > u.ranks[rr]:
		u.parents[rr] = lr
	default:
		u.parents[rr] = lr
		u.ranks[lr]++
	}
}

// MustNewUUIDv7 returns a UUIDv7 (matches Rust `Uuid::now_v7()`).
// On the (extremely unlikely) failure of `uuid.NewV7`, falls back to
// UUIDv4 — same panic-free behaviour the Rust impl gives.
func MustNewUUIDv7() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.New()
	}
	return id
}

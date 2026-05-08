package handlers

import "testing"

// Mirrors Rust handlers::branches::tests::detects_diverged_target_branch_versions.
func TestHasMergeConflictDetectsDivergedTargetBranchVersions(t *testing.T) {
	if hasMergeConflict(3, 5, 3) {
		t.Fatal("fast-forward case (target == source.base) must not conflict")
	}
	if hasMergeConflict(3, 5, 5) {
		t.Fatal("already-applied case (target == source.head) must not conflict")
	}
	if !hasMergeConflict(3, 5, 4) {
		t.Fatal("diverged target (4 ∉ {3,5}) must conflict")
	}
}

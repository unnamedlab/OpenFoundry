package marketplace

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/openfoundry/openfoundry-go/services/federation-product-exchange-service/internal/models"
)

func decodeDependencies(raw json.RawMessage) ([]models.DependencyRequirement, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return []models.DependencyRequirement{}, nil
	}
	var deps []models.DependencyRequirement
	if err := json.Unmarshal(raw, &deps); err != nil {
		return nil, fmt.Errorf("%w: dependencies must be an array", ErrValidation)
	}
	for i, dep := range deps {
		if strings.TrimSpace(dep.PackageSlug) == "" {
			return nil, fmt.Errorf("%w: dependency %d package_slug is required", ErrValidation, i)
		}
		if strings.TrimSpace(dep.VersionReq) == "" {
			return nil, fmt.Errorf("%w: dependency %s version_req is required", ErrValidation, dep.PackageSlug)
		}
	}
	return deps, nil
}

func defaultActivation() models.InstallActivation {
	notes := "No runtime activation hook is configured for this package kind yet."
	return models.InstallActivation{Kind: "marketplace_record", Status: "recorded", Notes: &notes}
}

func dependencyConflicts(deps []models.DependencyRequirement, installed map[string]string) []models.DependencyConflict {
	conflicts := []models.DependencyConflict{}
	for _, dep := range deps {
		installedVersion, ok := installed[dep.PackageSlug]
		if !ok || satisfiesVersionReq(installedVersion, dep.VersionReq) {
			continue
		}
		conflicts = append(conflicts, models.DependencyConflict{
			PackageSlug:      dep.PackageSlug,
			VersionReq:       dep.VersionReq,
			InstalledVersion: installedVersion,
			Message:          fmt.Sprintf("installed %s@%s does not satisfy %s", dep.PackageSlug, installedVersion, dep.VersionReq),
		})
	}
	return conflicts
}

func satisfiesVersionReq(version, req string) bool {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	req = strings.TrimSpace(req)
	if req == "" || req == "*" {
		return true
	}
	if strings.HasPrefix(req, "^") {
		base, ok := parseVersion(strings.TrimPrefix(req, "^"))
		actual, ok2 := parseVersion(version)
		return ok && ok2 && actual.major == base.major && compareVersion(actual, base) >= 0
	}
	if strings.HasPrefix(req, "~") {
		base, ok := parseVersion(strings.TrimPrefix(req, "~"))
		actual, ok2 := parseVersion(version)
		return ok && ok2 && actual.major == base.major && actual.minor == base.minor && compareVersion(actual, base) >= 0
	}
	return strings.TrimPrefix(req, "=") == version
}

type semVersion struct{ major, minor, patch int }

func parseVersion(raw string) (semVersion, bool) {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(raw), "v"), ".")
	if len(parts) < 1 || len(parts) > 3 {
		return semVersion{}, false
	}
	values := [3]int{}
	for i, part := range parts {
		if part == "" {
			return semVersion{}, false
		}
		value, err := strconv.Atoi(part)
		if err != nil {
			return semVersion{}, false
		}
		values[i] = value
	}
	return semVersion{major: values[0], minor: values[1], patch: values[2]}, true
}

func compareVersion(left, right semVersion) int {
	if left.major != right.major {
		return left.major - right.major
	}
	if left.minor != right.minor {
		return left.minor - right.minor
	}
	return left.patch - right.patch
}

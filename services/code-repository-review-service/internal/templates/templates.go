package templates

import (
	"fmt"
	"sort"
	"strings"
)

const DefaultID = "python-transform"

// Template is a built-in Code Repository seed template.
type Template struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Description      string            `json:"description"`
	LanguageTemplate string            `json:"language_template"`
	PackageKind      string            `json:"package_kind"`
	Skeleton         string            `json:"skeleton"`
	BuildCommand     []string          `json:"build_command"`
	Files            map[string]string `json:"-"`
}

// List returns the built-in templates in stable display order.
func List() []Template {
	ids := make([]string, 0, len(builtins))
	for id := range builtins {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Template, 0, len(ids))
	for _, id := range ids {
		out = append(out, clone(builtins[id]))
	}
	return out
}

// Get returns a built-in template by ID or alias.
func Get(id string) (Template, bool) {
	normalized := NormalizeID(id)
	t, ok := builtins[normalized]
	if !ok {
		return Template{}, false
	}
	return clone(t), true
}

// NormalizeID maps legacy/short names to the canonical built-in ID.
func NormalizeID(id string) string {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "", "python", "transforms-python":
		return "python-transform"
	case "java", "transforms-java":
		return "java-transform"
	case "sql", "transforms-sql":
		return "sql-transform"
	case "r", "transforms-r":
		return "r-transform"
	case "typescript", "typescript-function", "functions-typescript", "foundry-functions-typescript":
		return "typescript-function"
	default:
		return strings.ToLower(strings.TrimSpace(id))
	}
}

func clone(t Template) Template {
	files := make(map[string]string, len(t.Files))
	for path, content := range t.Files {
		files[path] = content
	}
	t.Files = files
	return t
}

func manifest(t Template) string {
	return fmt.Sprintf(`{
  "template_id": %q,
  "language_template": %q,
  "skeleton": %q,
  "build_command": %q
}
`, t.ID, t.LanguageTemplate, t.Skeleton, strings.Join(t.BuildCommand, " "))
}

var builtins = map[string]Template{
	"python-transform": newTemplate(Template{
		ID:               "python-transform",
		Name:             "Python transform",
		Description:      "transforms-python skeleton with sample input/output contracts and a pytest-backed build.",
		LanguageTemplate: "python-transform",
		PackageKind:      "transform",
		Skeleton:         "transforms-python",
		BuildCommand:     []string{"python", "-m", "pytest"},
		Files: map[string]string{
			"README.md": `# Python transform repository

Seeded from the OpenFoundry transforms-python template.

## Build

` + "```sh\npython -m pytest\n```\n",
			"pyproject.toml": `[project]
name = "openfoundry-python-transform"
version = "0.1.0"
description = "OpenFoundry Python transform template"
requires-python = ">=3.11"

[tool.pytest.ini_options]
pythonpath = ["src"]
testpaths = ["tests"]
`,
			"src/transforms_python/__init__.py": `from .transform import compute

__all__ = ["compute"]
`,
			"src/transforms_python/transform.py": `def compute(rows):
    """Example transform that keeps active records and adds a normalized name."""
    return [
        {**row, "normalized_name": row["name"].strip().lower()}
        for row in rows
        if row.get("active", True)
    ]
`,
			"tests/test_transform.py": `from transforms_python import compute


def test_compute_filters_and_normalizes_rows():
    rows = [
        {"id": 1, "name": " Alice ", "active": True},
        {"id": 2, "name": "Bob", "active": False},
    ]

    assert compute(rows) == [{"id": 1, "name": " Alice ", "active": True, "normalized_name": "alice"}]
`,
		},
	}),
	"java-transform": newTemplate(Template{
		ID:               "java-transform",
		Name:             "Java transform",
		Description:      "transforms-java Gradle skeleton with sample rows and a JUnit build.",
		LanguageTemplate: "java-transform",
		PackageKind:      "transform",
		Skeleton:         "transforms-java",
		BuildCommand:     []string{"./gradlew", "test"},
		Files: map[string]string{
			"README.md":       "# Java transform repository\n\nSeeded from the OpenFoundry transforms-java template.\n\n## Build\n\n```sh\n./gradlew test\n```\n",
			"settings.gradle": "pluginManagement { repositories { gradlePluginPortal(); mavenCentral() } }\ndependencyResolutionManagement { repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS); repositories { mavenCentral() } }\nrootProject.name = 'openfoundry-java-transform'\n",
			"build.gradle":    "plugins { id 'java' }\n\ngroup = 'dev.openfoundry.templates'\nversion = '0.1.0'\n\njava { toolchain { languageVersion = JavaLanguageVersion.of(17) } }\n\ndependencies { testImplementation 'org.junit.jupiter:junit-jupiter:5.10.3' }\n\ntest { useJUnitPlatform() }\n",
			"src/main/java/dev/openfoundry/transforms/CustomerTransform.java": `package dev.openfoundry.transforms;

import java.util.List;

public final class CustomerTransform {
    private CustomerTransform() {}

    public static List<String> activeCustomerNames(List<CustomerRow> rows) {
        return rows.stream()
                .filter(CustomerRow::active)
                .map(row -> row.name().trim().toLowerCase())
                .toList();
    }
}
`,
			"src/main/java/dev/openfoundry/transforms/CustomerRow.java": "package dev.openfoundry.transforms;\n\npublic record CustomerRow(long id, String name, boolean active) {}\n",
			"src/test/java/dev/openfoundry/transforms/CustomerTransformTest.java": `package dev.openfoundry.transforms;

import static org.junit.jupiter.api.Assertions.assertEquals;

import java.util.List;
import org.junit.jupiter.api.Test;

class CustomerTransformTest {
    @Test
    void filtersAndNormalizesActiveCustomers() {
        var rows = List.of(new CustomerRow(1, " Alice ", true), new CustomerRow(2, "Bob", false));
        assertEquals(List.of("alice"), CustomerTransform.activeCustomerNames(rows));
    }
}
`,
		},
	}),
	"r-transform": newTemplate(Template{
		ID:               "r-transform",
		Name:             "R transform",
		Description:      "transforms-r skeleton with testthat sample input/output checks.",
		LanguageTemplate: "r-transform",
		PackageKind:      "transform",
		Skeleton:         "transforms-r",
		BuildCommand:     []string{"Rscript", "-e", "testthat::test_dir('tests')"},
		Files: map[string]string{
			"README.md":                       "# R transform repository\n\nSeeded from the OpenFoundry transforms-r template.\n\n## Build\n\n```sh\nRscript -e \"testthat::test_dir('tests')\"\n```\n",
			"DESCRIPTION":                     "Package: openfoundryRTransform\nType: Package\nTitle: OpenFoundry R Transform Template\nVersion: 0.1.0\nEncoding: UTF-8\nSuggests: testthat\n",
			"R/transform.R":                   "compute <- function(rows) {\n  active <- rows[rows$active, , drop = FALSE]\n  active$normalized_name <- tolower(trimws(active$name))\n  active\n}\n",
			"tests/testthat/test-transform.R": "source('../../R/transform.R')\n\ntestthat::test_that('compute filters and normalizes rows', {\n  rows <- data.frame(id = c(1, 2), name = c(' Alice ', 'Bob'), active = c(TRUE, FALSE))\n  result <- compute(rows)\n  testthat::expect_equal(result$normalized_name, c('alice'))\n})\n",
		},
	}),
	"sql-transform": newTemplate(Template{
		ID:               "sql-transform",
		Name:             "SQL transform",
		Description:      "transforms-sql skeleton with a deterministic sample query and smoke check.",
		LanguageTemplate: "sql-transform",
		PackageKind:      "transform",
		Skeleton:         "transforms-sql",
		BuildCommand:     []string{"bash", "scripts/build.sh"},
		Files: map[string]string{
			"README.md":                   "# SQL transform repository\n\nSeeded from the OpenFoundry transforms-sql template.\n\n## Build\n\n```sh\nbash scripts/build.sh\n```\n",
			"transforms/customers.sql":    "-- @input customers\n-- @output active_customers\nselect\n  id,\n  lower(trim(name)) as normalized_name\nfrom customers\nwhere active = true;\n",
			"sample_inputs/customers.csv": "id,name,active\n1, Alice ,true\n2,Bob,false\n",
			"scripts/build.sh":            "#!/usr/bin/env bash\nset -euo pipefail\ntest -s transforms/customers.sql\ngrep -qi 'where active = true' transforms/customers.sql\necho 'SQL transform template smoke build passed'\n",
		},
	}),
	"typescript-function": newTemplate(Template{
		ID:               "typescript-function",
		Name:             "TypeScript function",
		Description:      "foundry-functions-typescript skeleton with sample function and TypeScript check.",
		LanguageTemplate: "typescript-function",
		PackageKind:      "function",
		Skeleton:         "foundry-functions-typescript",
		BuildCommand:     []string{"npm", "test"},
		Files: map[string]string{
			"README.md":               "# TypeScript function repository\n\nSeeded from the OpenFoundry foundry-functions-typescript template.\n\n## Build\n\n```sh\nnpm test\n```\n",
			"package.json":            "{\n  \"name\": \"openfoundry-typescript-function\",\n  \"version\": \"0.1.0\",\n  \"type\": \"module\",\n  \"scripts\": { \"test\": \"tsc --noEmit\" },\n  \"devDependencies\": { \"typescript\": \"^5.6.0\" }\n}\n",
			"tsconfig.json":           "{\n  \"compilerOptions\": {\n    \"target\": \"ES2022\",\n    \"module\": \"ES2022\",\n    \"moduleResolution\": \"Bundler\",\n    \"strict\": true,\n    \"noEmit\": true\n  },\n  \"include\": [\"src/**/*.ts\", \"tests/**/*.ts\"]\n}\n",
			"src/functions.ts":        "export interface Customer { id: number; name: string; active: boolean }\n\nexport function activeCustomerNames(customers: Customer[]): string[] {\n  return customers.filter((customer) => customer.active).map((customer) => customer.name.trim().toLowerCase());\n}\n",
			"tests/functions.test.ts": "import { activeCustomerNames } from '../src/functions';\n\nconst names = activeCustomerNames([{ id: 1, name: ' Alice ', active: true }, { id: 2, name: 'Bob', active: false }]);\n\nif (names.length !== 1 || names[0] !== 'alice') {\n  throw new Error(`unexpected names: ${names.join(',')}`);\n}\n",
		},
	}),
}

func newTemplate(t Template) Template {
	if t.Files == nil {
		t.Files = map[string]string{}
	}
	t.Files["openfoundry.template.json"] = manifest(t)
	return t
}

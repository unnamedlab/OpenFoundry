//! Foundry-Iceberg branch alias resolution.
//!
//! Per `Iceberg tables.md` § "Notable differences" / "Default branches":
//!
//! > Iceberg's main branch is called `main`, whereas Foundry's main
//! > branch is called `master`. In Foundry's integration with Iceberg,
//! > `main` and `master` are treated as the same.
//!
//! This module is the single source of truth for that rewrite. Every
//! REST handler that accepts a branch name from the wire goes through
//! [`resolve_branch_alias`]; the original input is preserved alongside
//! the rewritten value so handlers can emit the
//! `X-Foundry-Branch-Alias: master->main` response header for
//! transparency (audit + client-side debugging).

/// Header set on responses where a branch alias was applied. The value
/// follows the form `<input>-><resolved>` so a curious operator can
/// trace why their `master` request landed on Iceberg's `main`.
pub const ALIAS_HEADER: &str = "x-foundry-branch-alias";

/// Result of an alias rewrite. The two-tuple is intentional so callers
/// can decide whether to emit the [`ALIAS_HEADER`].
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AliasOutcome {
    pub input: String,
    pub resolved: String,
    pub aliased: bool,
}

impl AliasOutcome {
    pub fn header_value(&self) -> Option<String> {
        if self.aliased {
            Some(format!("{}->{}", self.input, self.resolved))
        } else {
            None
        }
    }
}

/// Map a wire-level branch name onto the canonical Iceberg name.
///
/// The function is deliberately allocation-free in the no-rewrite path
/// so callers can use the returned `&str` in tight inner loops.
pub fn resolve_branch_alias(input: &str) -> &str {
    match input {
        "master" => "main",
        other => other,
    }
}

/// Owned variant of [`resolve_branch_alias`] that records whether the
/// rewrite happened. Use this on the request path; emit
/// [`ALIAS_HEADER`] on the response when [`AliasOutcome::aliased`] is
/// true.
pub fn resolve_branch_alias_outcome(input: &str) -> AliasOutcome {
    let resolved = resolve_branch_alias(input);
    AliasOutcome {
        input: input.to_string(),
        resolved: resolved.to_string(),
        aliased: input != resolved,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn master_resolves_to_main() {
        assert_eq!(resolve_branch_alias("master"), "main");
    }

    #[test]
    fn other_branches_pass_through() {
        for branch in ["main", "feature-x", "release-2.0", "develop"] {
            assert_eq!(resolve_branch_alias(branch), branch);
        }
    }

    #[test]
    fn outcome_marks_aliased_only_for_master() {
        let outcome = resolve_branch_alias_outcome("master");
        assert!(outcome.aliased);
        assert_eq!(outcome.header_value().unwrap(), "master->main");

        let pass = resolve_branch_alias_outcome("feature-x");
        assert!(!pass.aliased);
        assert!(pass.header_value().is_none());
    }
}

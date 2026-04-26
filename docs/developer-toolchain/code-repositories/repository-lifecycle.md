# Repository lifecycle

Code repositories in OpenFoundry already look like a product surface, not just a storage layer.

## Current lifecycle

1. create a repository
2. create branches
3. create commits
4. inspect diffs and files
5. trigger CI
6. create merge requests and comments
7. merge changes

## Repository signals

These stages map directly onto handlers in `services/code-repo-service/src/handlers/*`.

# Identity and access

Identity and access are one of the strongest implemented capability areas in the current repo.

## Repository signals

`auth-service` already exposes first-class support for:

- registration and login
- JWT access and refresh flows
- MFA
- SSO provider management
- session management
- user, role, group, and permission administration
- control panel and admin-oriented surfaces

You can see the route surface in `services/auth-service/src/main.rs`.

## Domain building blocks

Relevant domain modules include:

- `domain/jwt.rs`
- `domain/rbac.rs`
- `domain/abac.rs`
- `domain/mfa.rs`
- `domain/saml.rs`
- `domain/oauth.rs`
- `domain/sessions.rs`

## Why this matters

This gives OpenFoundry a strong foundation for identity-aware operational workflows, not only for simple API authentication.

# Identity SCIM 2.0 Support

`identity-federation-service` exposes `/scim/v2` for upstream IdP
provisioning. The implementation is intentionally scoped to the
identity stores owned by the platform:

- `Users` map to `users`.
- `Groups` map to `groups` and `group_members`.
- `externalId` maps to `users.scim_external_id` and
  `groups.scim_external_id`; it is unique when present and makes
  create requests idempotent.
- User tenancy maps to `users.organization_id` through the
  OpenFoundry extension
  `urn:openfoundry:params:scim:schemas:extension:2.0:User`.

Supported filters:

- `Users`: `userName eq "..."`, `externalId eq "..."`.
- `Groups`: `displayName eq "..."`, `externalId eq "..."`.

Supported pagination:

- `startIndex` is 1-based.
- `count` is capped at 500.

Supported PATCH operations:

- `Users`: `replace` / `add` for `userName`, `active`, `name`,
  `emails`, `externalId`, and the OpenFoundry user extension.
- `Groups`: `add`, `replace`, and `remove` for `members`; `replace`
  for `displayName` and `externalId`.

Unsupported SCIM features return RFC 7644 error bodies instead of
internal stubs:

- Bulk operations.
- Sorting.
- ETags.
- Password changes.
- Arbitrary SCIM filters beyond the equality filters above.

Tenancy extension examples:

```json
{
  "urn:openfoundry:params:scim:schemas:extension:2.0:User": {
    "organizationId": "018f6b7c-0000-7000-9000-000000000001"
  }
}
```

```json
{
  "urn:openfoundry:params:scim:schemas:extension:2.0:User": {
    "organizationSlug": "acme"
  }
}
```

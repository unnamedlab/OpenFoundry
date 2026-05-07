// media-sets-service bundled defaults — mirrors
// services/media-sets-service/src/domain/cedar.rs MEDIA_DEFAULT_POLICIES
// 1:1. The clearance check is `principal.clearances.containsAll(
// resource.markings)`; entity attributes carry already-lowercased
// marking ids so the check is case-insensitive.

@id("media-set-view")
permit (
    principal,
    action == Action::"media_set::view",
    resource is MediaSet
) when {
    principal.tenant == resource.tenant &&
    principal.clearances.containsAll(resource.markings)
};

@id("media-set-manage")
permit (
    principal,
    action == Action::"media_set::manage",
    resource is MediaSet
) when {
    principal.tenant == resource.tenant &&
    (principal.roles.contains("editor") || principal.roles.contains("admin")) &&
    principal.clearances.containsAll(resource.markings)
};

@id("media-set-delete")
permit (
    principal,
    action == Action::"media_set::delete",
    resource is MediaSet
) when {
    principal.tenant == resource.tenant &&
    principal.roles.contains("admin") &&
    principal.clearances.containsAll(resource.markings)
};

@id("media-item-read")
permit (
    principal,
    action == Action::"media_item::read",
    resource is MediaItem
) when {
    principal.tenant == resource.tenant &&
    principal.clearances.containsAll(resource.markings)
};

@id("media-item-write")
permit (
    principal,
    action == Action::"media_item::write",
    resource is MediaItem
) when {
    principal.tenant == resource.tenant &&
    (principal.roles.contains("editor") || principal.roles.contains("admin")) &&
    principal.clearances.containsAll(resource.markings)
};

@id("media-item-delete")
permit (
    principal,
    action == Action::"media_item::delete",
    resource is MediaItem
) when {
    principal.tenant == resource.tenant &&
    (principal.roles.contains("editor") || principal.roles.contains("admin")) &&
    principal.clearances.containsAll(resource.markings)
};

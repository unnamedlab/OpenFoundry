import { describe, expect, it } from "vitest";

import {
  buildOntologyResourceRegistry,
  deriveOntologyArtifact,
  formatPropertyValue,
  linkTypeCardinalityLabel,
  linkTypeEndpointLabels,
  linkTypeHasDatasourceMapping,
  objectTypeAPIName,
  objectTypeGeoPointPropertyNames,
  objectTypeGeoShapePropertyNames,
  objectTypePluralDisplayName,
  objectTypePrimaryKey,
  objectTypeRID,
  objectTypeSearchablePropertyNames,
  objectTypeTitleProperty,
  objectViewVisibleProperties,
  propertyConditionalStyle,
  propertyTypeMetadata,
  type ObjectType,
  type Property,
} from "./ontology";

const now = "2026-05-11T00:00:00Z";

function property(overrides: Partial<Property>): Property {
  return {
    id: overrides.id ?? crypto.randomUUID(),
    object_type_id: overrides.object_type_id ?? "Trail",
    name: overrides.name ?? "label",
    display_name: overrides.display_name ?? overrides.name ?? "Label",
    description: overrides.description ?? "",
    property_type: overrides.property_type ?? "string",
    required: overrides.required ?? false,
    unique_constraint: overrides.unique_constraint ?? false,
    time_dependent: overrides.time_dependent ?? false,
    default_value: overrides.default_value ?? null,
    validation_rules: overrides.validation_rules ?? null,
    inline_edit_config: overrides.inline_edit_config ?? null,
    created_at: overrides.created_at ?? now,
    updated_at: overrides.updated_at ?? now,
    ...overrides,
  };
}

function objectType(overrides: Partial<ObjectType> = {}): ObjectType {
  return {
    id: "Trail",
    name: "Trail",
    display_name: "Trail",
    description: "",
    primary_key_property: "id",
    icon: "walk",
    color: "#0f766e",
    owner_id: "test",
    created_at: now,
    updated_at: now,
    ...overrides,
  };
}

describe("ontology object type metadata helpers", () => {
  it("keeps stable aliases for Foundry-like metadata", () => {
    const type = objectType({
      properties: [
        property({ name: "label", property_type: "string" }),
        property({ name: "trailhead", property_type: "geopoint" }),
        property({ name: "route", property_type: "geojson" }),
      ],
      title_property: "label",
    });

    expect(objectTypeRID(type)).toBe("ri.ontology.main.object-type.Trail");
    expect(objectTypeAPIName(type)).toBe("Trail");
    expect(objectTypePluralDisplayName(type)).toBe("Trails");
    expect(objectTypePrimaryKey(type)).toBe("id");
    expect(objectTypeTitleProperty(type)).toBe("label");
    expect(objectTypeSearchablePropertyNames(type)).toEqual(["label", "id"]);
    expect(objectTypeGeoPointPropertyNames(type)).toEqual(["trailhead"]);
    expect(objectTypeGeoShapePropertyNames(type)).toEqual(["route"]);
  });

  it("honors backend-provided metadata over derived fallbacks", () => {
    const type = objectType({
      rid: "ri.custom.object-type.trail",
      api_name: "TrailApi",
      plural_display_name: "Trailheads",
      primary_key: "trail_id",
      title_property: "trail_name",
      searchable_property_names: ["trail_name"],
      geopoint_property_names: ["start_point"],
      geoshape_property_names: ["route_shape"],
    });

    expect(objectTypeRID(type)).toBe("ri.custom.object-type.trail");
    expect(objectTypeAPIName(type)).toBe("TrailApi");
    expect(objectTypePluralDisplayName(type)).toBe("Trailheads");
    expect(objectTypePrimaryKey(type)).toBe("trail_id");
    expect(objectTypeTitleProperty(type)).toBe("trail_name");
    expect(objectTypeSearchablePropertyNames(type)).toEqual(["trail_name"]);
    expect(objectTypeGeoPointPropertyNames(type)).toEqual(["start_point"]);
    expect(objectTypeGeoShapePropertyNames(type)).toEqual(["route_shape"]);
  });
});

describe("ontology property metadata helpers", () => {
  it("derives base type semantics for advanced property types", () => {
    expect(
      propertyTypeMetadata(property({ property_type: "geopoint" })),
    ).toMatchObject({
      base_type: "geopoint",
      type_family: "geospatial",
      value_shape: "lat-lon-object",
      filterable: true,
      sortable: false,
    });
    expect(
      propertyTypeMetadata(property({ property_type: "geojson" })),
    ).toMatchObject({
      base_type: "geoshape",
      type_family: "geospatial",
    });
    expect(
      propertyTypeMetadata(property({ property_type: "vector" })),
    ).toMatchObject({
      base_type: "vector",
      type_family: "semantic",
      array_allowed: false,
    });
    expect(
      propertyTypeMetadata(property({ property_type: "time_series" })),
    ).toMatchObject({
      base_type: "time_series",
      type_family: "timeseries",
      array_allowed: false,
    });
    expect(
      propertyTypeMetadata(property({ property_type: "decimal" })),
    ).toMatchObject({
      base_type: "decimal",
      type_family: "numeric",
      aggregatable: true,
      formatting_eligible: true,
      primary_key_eligible: false,
    });
    expect(
      propertyTypeMetadata(property({ property_type: "geohash" })),
    ).toMatchObject({
      base_type: "geohash",
      type_family: "geospatial",
      title_key_eligible: true,
      object_security_eligible: true,
    });
    expect(
      propertyTypeMetadata(property({ property_type: "array<string>" })),
    ).toMatchObject({
      base_type: "array",
      type_family: "collection",
      array_item_type: "string",
      primary_key_eligible: false,
      title_key_eligible: false,
    });
  });

  it("honors backend-provided property metadata", () => {
    const metadata = propertyTypeMetadata(
      property({
        property_type: "string",
        base_type: "media_reference",
        type_family: "media",
        type_display_name: "Media reference",
        value_shape: "media-reference",
        array_allowed: true,
        searchable: false,
        filterable: true,
        sortable: false,
        aggregatable: false,
        primary_key_eligible: false,
        title_key_eligible: false,
        formatting_eligible: true,
        object_security_eligible: false,
        prominent_eligible: true,
        semantic_hints: ["media"],
      }),
    );

    expect(metadata.base_type).toBe("media_reference");
    expect(metadata.type_family).toBe("media");
    expect(metadata.formatting_eligible).toBe(true);
    expect(metadata.prominent_eligible).toBe(true);
    expect(metadata.semantic_hints).toEqual(["media"]);
  });
});

describe("ontology property formatting helpers", () => {
  it("orders prominent properties and hides hidden properties for Object Views", () => {
    const visible = objectViewVisibleProperties([
      property({ id: "normal", name: "normal", display_mode: "normal" }),
      property({ id: "hidden", name: "hidden", display_mode: "hidden" }),
      property({ id: "prominent", name: "prominent", display_mode: "prominent" }),
    ]);

    expect(visible.map((entry) => entry.name)).toEqual(["prominent", "normal"]);
  });

  it("formats values and applies conditional formatting rules", () => {
    const amount = property({
      property_type: "decimal",
      value_formatting: {
        style: "currency",
        currency: "USD",
        maximum_fraction_digits: 0,
      },
      conditional_formatting: [
        { operator: "gte", value: 1000, color: "#065f46", font_weight: "700" },
      ],
    });

    expect(formatPropertyValue(amount, 1234.5)).toBe("$1,235");
    expect(propertyConditionalStyle(amount, 1234.5)).toMatchObject({
      color: "#065f46",
      fontWeight: "700",
    });
  });
});

describe("ontology link type helpers", () => {
  it("formats cardinality, endpoint labels, and many-to-many datasource mapping state", () => {
    const link = {
      id: "link-1",
      name: "TrailRace",
      display_name: "Trail race",
      description: "",
      source_type_id: "Trail",
      target_type_id: "Race",
      cardinality: "many_to_many",
      label: "has races",
      reverse_label: "uses trail",
      link_datasource_mapping: {
        datasource_id: "dataset.links",
        source_key: "trail_id",
        target_key: "race_id",
      },
      owner_id: "owner-1",
      created_at: now,
      updated_at: now,
    };

    expect(linkTypeCardinalityLabel(link.cardinality)).toBe("Many-to-many");
    expect(linkTypeEndpointLabels(link)).toEqual({
      forward: "has races",
      reverse: "uses trail",
    });
    expect(linkTypeHasDatasourceMapping(link)).toBe(true);
    expect(
      linkTypeHasDatasourceMapping({
        cardinality: "many_to_many",
        link_datasource_mapping: { datasource_id: "dataset.links" },
      }),
    ).toBe(false);
    expect(
      linkTypeHasDatasourceMapping({
        cardinality: "one_to_many",
        link_datasource_mapping: null,
      }),
    ).toBe(true);
  });
});

describe("ontology space-scoped artifact helpers", () => {
  it("derives private ontology metadata from a single owning space project", () => {
    const ontology = deriveOntologyArtifact({
      projects: [
        {
          id: "project-1",
          slug: "trail-running",
          display_name: "Trail Running",
          description: "Demo ontology placement",
          workspace_slug: "trail-space",
          owner_id: "owner-1",
          created_at: now,
          updated_at: now,
        },
      ],
      objectTypeCount: 2,
      linkTypeCount: 1,
    });

    expect(ontology).toMatchObject({
      id: "ontology.trail-space",
      display_name: "Trail Running",
      owning_space_slug: "trail-space",
      access_mode: "private",
      placement: {
        project_id: "project-1",
        folder_path: "/trail-space/ontology",
      },
    });
    expect(ontology.organizations).toEqual([
      {
        id: "org.trail-space",
        display_name: "Trail Space organization",
        marking: "trail-space",
      },
    ]);
    expect(ontology.linked_resources).toEqual([
      { resource_kind: "link_type", count: 1 },
      { resource_kind: "object_type", count: 2 },
    ]);
  });

  it("marks an ontology as shared when visible projects span organizations", () => {
    const ontology = deriveOntologyArtifact({
      projects: [
        {
          id: "p1",
          slug: "core",
          display_name: "Core",
          description: "",
          workspace_slug: "alpha",
          owner_id: "u1",
          created_at: now,
          updated_at: now,
        },
        {
          id: "p2",
          slug: "shared",
          display_name: "Shared",
          description: "",
          workspace_slug: "beta",
          owner_id: "u2",
          created_at: now,
          updated_at: now,
        },
      ],
      resourceBindings: [
        {
          project_id: "p1",
          resource_kind: "object_type",
          resource_id: "ot1",
          bound_by: "u1",
          created_at: now,
        },
        {
          project_id: "p1",
          resource_kind: "object_type",
          resource_id: "ot2",
          bound_by: "u1",
          created_at: now,
        },
      ],
      interfaceCount: 1,
    });

    expect(ontology.access_mode).toBe("shared");
    expect(ontology.organizations.map((org) => org.marking)).toEqual([
      "alpha",
      "beta",
    ]);
    expect(ontology.linked_resources).toContainEqual({
      resource_kind: "object_type",
      count: 2,
    });
    expect(ontology.linked_resources).toContainEqual({
      resource_kind: "interface",
      count: 1,
    });
  });
});

describe("ontology resource registry helpers", () => {
  it("normalizes ontology resources into first-class registry entries", () => {
    const project = {
      id: "project-1",
      slug: "trail-running",
      display_name: "Trail Running",
      description: "",
      workspace_slug: "trail-space",
      owner_id: "owner-1",
      created_at: now,
      updated_at: now,
    };
    const ontology = deriveOntologyArtifact({ projects: [project] });
    const trailType = objectType({
      id: "Trail",
      name: "TrailApi",
      display_name: "Trail",
      plural_display_name: "Trails",
      description: "Trail object type",
      backing_dataset_id: "dataset-1",
      status: "experimental",
    });
    const bindings = [
      {
        project_id: "project-1",
        resource_kind: "object_type",
        resource_id: "Trail",
        bound_by: "owner-1",
        created_at: now,
      },
    ];

    const registry = buildOntologyResourceRegistry({
      ontology,
      projects: [project],
      resourceBindings: bindings,
      objectTypes: [trailType],
      linkTypes: [
        {
          id: "link-1",
          name: "TrailToRace",
          display_name: "Trail to Race",
          description: "",
          source_type_id: "Trail",
          target_type_id: "Race",
          cardinality: "many_to_many",
          owner_id: "owner-1",
          created_at: now,
          updated_at: now,
        },
      ],
      actionTypes: [
        {
          id: "action-1",
          name: "UpdateTrail",
          display_name: "Update trail",
          description: "",
          object_type_id: "Trail",
          operation_kind: "update_object",
          input_schema: [],
          form_schema: {},
          config: {},
          confirmation_required: false,
          permission_key: null,
          authorization_policy: {},
          owner_id: "owner-1",
          created_at: now,
          updated_at: now,
        },
      ],
      interfaces: [],
      sharedPropertyTypes: [],
      objectTypeGroups: [
        {
          id: "group-1",
          name: "trail_assets",
          display_name: "Trail assets",
          description: "Trail operating model",
          visibility: "normal",
          status: "active",
          owner_id: "owner-1",
          object_type_ids: ["Trail"],
          object_type_count: 1,
          created_at: now,
          updated_at: now,
        },
      ],
      objectViews: [
        {
          id: "view-1",
          name: "TrailFullView",
          display_name: "Trail full view",
          object_type_id: "Trail",
          mode: "standard",
          form_factor: "full",
          published: true,
          owner_id: "owner-1",
          created_at: now,
          updated_at: now,
        },
      ],
    });

    expect(registry).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          resource_kind: "object_type",
          resource_id: "Trail",
          api_name: "TrailApi",
          display_name: "Trail",
          plural_display_name: "Trails",
          project_id: "project-1",
          project_display_name: "Trail Running",
          status: "experimental",
          usage_count: 3,
          backing_datasource_id: "dataset-1",
        }),
        expect.objectContaining({
          resource_kind: "datasource_registration",
          resource_id: "Trail:dataset-1",
          display_name: "Trail datasource",
          backing_datasource_id: "dataset-1",
        }),
        expect.objectContaining({
          resource_kind: "object_type_group",
          resource_id: "group-1",
          usage_count: 1,
          linked_resource_count: 1,
        }),
        expect.objectContaining({
          resource_kind: "core_object_view",
          resource_id: "view-1",
          linked_resource_count: 1,
        }),
      ]),
    );
  });
});

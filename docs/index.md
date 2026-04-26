---
layout: home

hero:
  name: OpenFoundry
  text: Technical Documentation
  tagline: "Capability-oriented documentation for an open-source operational data platform inspired by the information architecture of Foundry docs."
  image:
    src: /logo.png
    alt: OpenFoundry
  actions:
    - theme: brand
      text: Explore capabilities
      link: /ontology-building/
    - theme: alt
      text: Getting started
      link: /getting-started/
    - theme: alt
      text: Architecture center
      link: /architecture-center/

features:
  - title: Capability-based structure
    details: "Browse docs the same way platform teams think about the product: AI, data connectivity, ontology, developer workflows, analytics, governance, and delivery."
  - title: OpenFoundry-specific mapping
    details: "Each section maps the capability model onto the actual services, contracts, SDKs, infra, and frontend modules present in this repository."
  - title: Built in phases
    details: "The information architecture is being expanded iteratively, starting with the highest-signal sections such as Ontology building and platform architecture."
---

## Capability Areas

OpenFoundry now organizes its official documentation around these top-level capability areas:

- [AI Platform (AIP)](/ai-platform/)
- [Data connectivity & integration](/data-connectivity/)
- [Model connectivity & development](/model-connectivity/)
- [Ontology building](/ontology-building/)
- [Developer toolchain](/developer-toolchain/)
- [Use case development](/use-case-development/)
- [Observability](/observability/)
- [Analytics](/analytics/)
- [Product delivery](/product-delivery/)
- [Security & governance](/security-governance/)
- [Management & enablement](/management-enablement/)

## What This Site Covers

This documentation still stays grounded in the repository itself:

- how platform capabilities map onto `services/*`, `libs/*`, `apps/web`, `proto/*`, and `infra/*`
- how contributors build, test, and ship changes
- how ontology-centric workflows fit into the current OpenFoundry architecture
- how docs, contracts, SDKs, and deployment assets are delivered

## Recommended Reading Order

1. [Getting started](/getting-started/) for contributor orientation.
2. [Ontology building](/ontology-building/) for the core platform semantics.
3. [Architecture center](/architecture-center/) for runtime and contract boundaries.
4. [Platform updates](/platform-updates/) for release-facing changes in the docs set.

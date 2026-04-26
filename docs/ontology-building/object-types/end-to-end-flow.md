# End-to-end object type flow

This is the clearest current end-to-end semantic flow in the repo because the P3 smoke test already exercises it.

## Flow taken from P3 smoke

The scenario in `smoke/scenarios/p3-semantic-governance-critical-path.json` currently does the following:

1. create an interface
2. add a property to that interface
3. create a case object type
4. attach the interface to the case type
5. add multiple properties to the case type
6. create supporting object types and objects
7. use those semantics in downstream object interactions

## Sequence

```text
operator
  -> gateway
  -> ontology-service: create interface
  -> ontology-service: create object type
  -> ontology-service: attach interface to type
  -> ontology-service: add properties
  -> ontology-service: create objects
  -> downstream search/rules/apps consume the resulting semantic model
```

## Why this is important

This is already more than static metadata. It shows OpenFoundry moving toward a model where ontology definitions directly power operational objects and behavior.

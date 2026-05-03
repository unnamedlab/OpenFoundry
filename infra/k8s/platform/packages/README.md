# Platform Runtime Packages

This directory contains source packages that are bundled by platform Helm
charts but are easier to review and operate as first-class source trees.

| Directory | Consumed by |
| --- | --- |
| `vespa-app/` | `../charts/vespa`, mirrored into `../charts/vespa/files/` for Helm packaging |

Keep each package's README authoritative about how it is mirrored or
embedded by the consuming chart.

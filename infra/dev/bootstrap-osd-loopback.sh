#!/usr/bin/env bash
# infra/dev/bootstrap-osd-loopback.sh — pre-create the 25G loopback device on
# each Lima node that infra/dev/ceph-single-node.yaml expects to find at
# /dev/loop0. Safe to re-run; both `truncate` and `losetup -j` are idempotent.
#
# Producción usa block devices reales en cada nodo; este script existe SOLO
# para que dev/Lima reproduzca la topología (same Rook-Ceph operator, same
# Ceph image, same CRDs) sin pedir un disco crudo extra a la VM.
set -euo pipefail

NODES=("${@:-k3s-master k3s-node1 k3s-node2}")

for node in "${NODES[@]}"; do
  echo "=== $node ==="
  limactl shell "$node" -- sudo bash -c '
    set -e
    mkdir -p /var/lib/rook-osd
    if [ ! -f /var/lib/rook-osd/disk.img ]; then
      truncate -s 25G /var/lib/rook-osd/disk.img
    fi
    if ! losetup -j /var/lib/rook-osd/disk.img | grep -q loop; then
      losetup -f /var/lib/rook-osd/disk.img
    fi
    losetup -j /var/lib/rook-osd/disk.img
  '
done

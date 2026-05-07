"""Generated proto stubs.

The grpc-generated files import each other as ``from runtime import ...``
which assumes ``runtime`` is a top-level package. Prepend this directory
to ``sys.path`` so the absolute imports resolve before anyone touches
``python_runtime_pb2``.
"""

from __future__ import annotations

import os as _os
import sys as _sys

_THIS_DIR = _os.path.dirname(_os.path.abspath(__file__))
if _THIS_DIR not in _sys.path:
    _sys.path.insert(0, _THIS_DIR)

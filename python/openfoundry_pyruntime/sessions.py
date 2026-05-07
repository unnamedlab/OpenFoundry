"""Stateful per-session globals dicts for notebook-runtime."""

from __future__ import annotations

import threading
import time
from typing import Any
from uuid import UUID


class SessionRegistry:
    """Thread-safe map of UUID → globals dict.

    Mirrors `PythonSessions = Arc<RwLock<HashMap<Uuid, Arc<Py<PyDict>>>>>`
    in the Rust notebook kernel.
    """

    def __init__(self, max_idle_seconds: float | None = None) -> None:
        self._lock = threading.Lock()
        self._sessions: dict[UUID, dict[str, Any]] = {}
        self._touched: dict[UUID, float] = {}
        self._max_idle = max_idle_seconds

    def ensure(self, session_id: UUID) -> dict[str, Any]:
        with self._lock:
            self._evict_locked()
            globals_dict = self._sessions.get(session_id)
            if globals_dict is None:
                globals_dict = {"__builtins__": __builtins__}
                self._sessions[session_id] = globals_dict
            self._touched[session_id] = time.monotonic()
            return globals_dict

    def get(self, session_id: UUID) -> dict[str, Any] | None:
        with self._lock:
            globals_dict = self._sessions.get(session_id)
            if globals_dict is not None:
                self._touched[session_id] = time.monotonic()
            return globals_dict

    def drop(self, session_id: UUID) -> None:
        with self._lock:
            self._sessions.pop(session_id, None)
            self._touched.pop(session_id, None)

    def _evict_locked(self) -> None:
        if self._max_idle is None:
            return
        cutoff = time.monotonic() - self._max_idle
        expired = [sid for sid, ts in self._touched.items() if ts < cutoff]
        for sid in expired:
            self._sessions.pop(sid, None)
            self._touched.pop(sid, None)

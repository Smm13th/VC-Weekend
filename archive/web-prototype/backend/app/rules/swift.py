"""SWIFT/BIC."""
from __future__ import annotations

import re

from .. import refdata

SWIFT_RE = re.compile(r'^[A-Z]{4}[A-Z]{2}[A-Z0-9]{2}([A-Z0-9]{3})?$')


def normalize_swift(s: str | None) -> str:
    return re.sub(r'[\s ]', '', s or '').upper()


def validate_swift(s: str | None) -> dict:
    n = normalize_swift(s)
    ok = bool(SWIFT_RE.match(n))
    return {'ok': ok, 'normalized': n, 'country': n[4:6] if ok else None,
            'error': None if ok else 'format', 'bank': refdata.bank_by_swift(n) if ok else None}

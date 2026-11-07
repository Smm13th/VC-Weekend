"""Справочники из app/data/*.json (общие с фронтендом через alias @data)."""
from __future__ import annotations

import json
from functools import lru_cache
from pathlib import Path

DATA_DIR = Path(__file__).parent / 'data'
ROOT = Path(__file__).resolve().parents[2]
SAMPLES_DIR = ROOT / 'frontend' / 'public' / 'samples'
EVAL_DIR = Path(__file__).resolve().parents[1] / 'eval'


@lru_cache(maxsize=None)
def load(name: str):
    """load('accounts') -> содержимое data/accounts.json"""
    return json.loads((DATA_DIR / f'{name}.json').read_text(encoding='utf-8'))


def rates() -> dict[str, float]:
    return load('rates')['rates']


def accounts() -> list[dict]:
    return load('accounts')['accounts']


def client() -> dict:
    return load('accounts')['client']


def tariffs() -> dict:
    return load('tariffs')


def bank_by_swift(swift: str) -> dict | None:
    s = swift.upper()
    for b in load('banks')['items']:
        if b['swift'] == s[:8] or b['swift'] == s:
            return b
    return None

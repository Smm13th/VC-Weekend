"""Даты: разбор dd.mm.yyyy, рабочие дни, даты валютирования."""
from __future__ import annotations

import re
from datetime import date, datetime, timedelta

from .. import refdata
from ..models import ValueDates

DATE_RE = re.compile(r'\b(\d{1,2})[./](\d{1,2})[./](\d{4})\b')


def parse_date(s) -> str | None:
    """'15.09.2026' или '2026-09-15' -> '2026-09-15'"""
    if not s:
        return None
    if isinstance(s, date):
        return s.isoformat()
    s = str(s).strip()
    m = re.fullmatch(r'(\d{4})-(\d{2})-(\d{2})', s)
    if m:
        return s
    m = DATE_RE.search(s)
    if not m:
        return None
    try:
        return date(int(m.group(3)), int(m.group(2)), int(m.group(1))).isoformat()
    except ValueError:
        return None


def to_date(s) -> date | None:
    iso = parse_date(s)
    return date.fromisoformat(iso) if iso else None


def is_business(d: date) -> bool:
    return d.weekday() < 5


def next_business(d: date) -> date:
    d += timedelta(days=1)
    while not is_business(d):
        d += timedelta(days=1)
    return d


def cutoff_for(currency: str) -> str:
    c = refdata.tariffs().get('cutoff', {})
    return (c.get(currency) or c.get('USD') or {'time': '15:00'})['time']


def value_dates(currency: str, today, now_time: str | None = None) -> ValueDates:
    """urgent: сегодня, если рабочий день и до cutoff, иначе следующий рабочий день.
    standard: следующий рабочий день после дня обработки (после cutoff получается через один)."""
    d = to_date(today) if not isinstance(today, date) else today
    if d is None:
        d = date.today()
    cutoff = cutoff_for(currency)
    after = False
    if now_time:
        after = now_time[:5] > cutoff
    base = d if (is_business(d) and not after) else next_business(d)
    return ValueDates(standard=next_business(base).isoformat(), urgent=base.isoformat(), cutoff=cutoff)


def today_iso(case_today: str | None) -> str:
    return parse_date(case_today) or datetime.now().date().isoformat()

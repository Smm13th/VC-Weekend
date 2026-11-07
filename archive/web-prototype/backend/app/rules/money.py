"""Суммы и курсы."""
from __future__ import annotations

import re

from .. import refdata

_NUM = re.compile(r'\d[\d\s   ,.\']*')


def parse_amount(s) -> float | None:
    """'51,000.00', '51 000,00', 'USD 51,000.00', '51.000,00', '51000' -> 51000.0"""
    if s is None:
        return None
    if isinstance(s, (int, float)):
        return float(s)
    m = _NUM.search(str(s))
    if not m:
        return None
    t = re.sub(r'[\s   \']', '', m.group(0)).rstrip('.,')
    if not t:
        return None
    neg = bool(re.search(r'-\s*$', str(s)[:m.start()]))
    if ',' in t and '.' in t:
        dec = ',' if t.rfind(',') > t.rfind('.') else '.'
        thou = '.' if dec == ',' else ','
        t = t.replace(thou, '').replace(dec, '.')
    elif ',' in t:
        parts = t.split(',')
        if len(parts) == 2 and len(parts[1]) in (1, 2):
            t = t.replace(',', '.')
        else:
            t = t.replace(',', '')
    elif t.count('.') > 1:
        t = t.replace('.', '')
    try:
        v = float(t)
    except ValueError:
        return None
    return -v if neg else v


def rate(cur: str) -> float:
    r = refdata.rates()
    if cur not in r:
        raise KeyError(f'нет курса для {cur}')
    return r[cur]


def convert(amount: float, frm: str, to: str) -> float:
    if frm == to:
        return round(float(amount), 2)
    return round(float(amount) * rate(frm) / rate(to), 2)


def spread_pct() -> float:
    return float(refdata.load('rates').get('conversionSpreadPct', 0))

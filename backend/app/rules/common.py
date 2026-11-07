"""Общие помощники правил: ссылки на источник, форматирование чисел и дат по-русски."""
from __future__ import annotations

import re
from datetime import date

from ..models import Finding, ParsedDoc, SourceRef


def block_at(doc: ParsedDoc, pos: int):
    for b in doc.blocks:
        if b.start <= pos < b.end or (b.start == b.end == pos):
            return b
    return None


def ref(doc: ParsedDoc, start: int, end: int) -> SourceRef:
    """SourceRef с точной цитатой; пробелы по краям обрезаются."""
    while start < end and doc.text[start].isspace():
        start += 1
    while end > start and doc.text[end - 1].isspace():
        end -= 1
    b = block_at(doc, start)
    return SourceRef(doc_id=doc.id, start=start, end=end, quote=doc.text[start:end], page=b.page if b else None)


def block_ref(doc: ParsedDoc, b) -> SourceRef:
    return ref(doc, b.start, b.end)


def line_spans(doc: ParsedDoc):
    """(start, end, block) для каждой строки текста (строки внутри ячеек тоже отдельные)."""
    for b in doc.blocks:
        s = b.start
        for line in doc.text[b.start:b.end].split('\n'):
            yield s, s + len(line), b
            s += len(line) + 1


def fmt_num(x: float, decimals: int = 2) -> str:
    s = f'{abs(x):,.{decimals}f}'.replace(',', ' ').replace('.', ',')
    return ('-' if x < 0 else '') + s


def fmt_money(x: float, cur: str | None = None, decimals: int = 2) -> str:
    return fmt_num(x, decimals) + (f' {cur}' if cur else '')


def fmt_date(iso: str | None) -> str:
    if not iso:
        return ''
    try:
        d = date.fromisoformat(iso)
        return d.strftime('%d.%m.%Y')
    except ValueError:
        return iso


def doc_label(doc: ParsedDoc) -> str:
    return {'contract': 'договоре', 'invoice': 'инвойсе', 'letter': 'письме'}.get(doc.kind, f'документе {doc.name}')


def doc_title(doc: ParsedDoc) -> str:
    return {'contract': 'договор', 'invoice': 'инвойс', 'letter': 'письмо'}.get(doc.kind, doc.name)


def finding(rule, doc_id, stage, severity, title, detail, sources=None, field=None, extra=None, **kw) -> Finding:
    fid = f'{rule}:{doc_id}' + (f':{extra}' if extra else '')
    return Finding(id=fid, rule=rule, stage=stage, severity=severity, title=title, detail=detail,
                   sources=[s for s in (sources or []) if s is not None], field=field, **kw)


def norm_name(s: str | None) -> str:
    return re.sub(r'[^0-9a-zа-яё]+', '', (s or '').lower())


PLACEHOLDER = re.compile(r'\[[^\]]{2,80}\]')


def has_placeholder(s: str) -> bool:
    return bool(PLACEHOLDER.search(s or ''))

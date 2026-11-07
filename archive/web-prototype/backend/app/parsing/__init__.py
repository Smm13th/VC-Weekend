"""Разбор документов (DOCX, PDF, TXT) в ParsedDoc: плоский текст + блоки с точными позициями."""
from __future__ import annotations

import io
import re

from ..models import DocBlock, DocKind, ParsedDoc

CYR = re.compile(r'[А-Яа-яЁё]')
_SPACES = re.compile(r'[ \t   ]{2,}')


class _Builder:
    def __init__(self):
        self.parts: list[str] = []
        self.blocks: list[DocBlock] = []
        self.pos = 0

    def add(self, text: str, **kw):
        if not text.strip():
            return
        if self.parts:
            self.parts.append('\n')
            self.pos += 1
        self.parts.append(text)
        self.blocks.append(DocBlock(start=self.pos, end=self.pos + len(text), **kw))
        self.pos += len(text)

    @property
    def text(self) -> str:
        return ''.join(self.parts)


def _clean_line(s: str) -> str:
    return _SPACES.sub(' ', s.replace(' ', ' ')).strip()


def _cell_text(cell) -> str:
    paras = [p.text.rstrip() for p in cell.paragraphs]
    while paras and not paras[-1].strip():
        paras.pop()
    while paras and not paras[0].strip():
        paras.pop(0)
    return '\n'.join(paras)


def _parse_docx(data: bytes) -> tuple[str, list[DocBlock], int]:
    from docx import Document
    from docx.table import Table
    from docx.text.paragraph import Paragraph

    doc = Document(io.BytesIO(data))
    b = _Builder()
    row_idx = 0
    for child in doc.element.body.iterchildren():
        tag = child.tag.rsplit('}', 1)[-1]
        if tag == 'p':
            b.add(Paragraph(child, doc).text.rstrip())
        elif tag == 'tbl':
            table = Table(child, doc)
            rows = []
            for row in table.rows:
                seen, cells = set(), []
                for c in row.cells:   # объединённые ячейки python-docx отдаёт повторно
                    if id(c._tc) in seen:
                        continue
                    seen.add(id(c._tc))
                    cells.append(_cell_text(c))
                rows.append(cells)
            two = all(len(r) == 2 for r in rows) and rows
            bilingual = bool(two) and any(CYR.search(r[1]) for r in rows)
            for cells in rows:
                for ci, t in enumerate(cells):
                    lang = None
                    if bilingual:
                        lang = 'en' if ci == 0 else 'ru'
                    b.add(t, row=row_idx, lang=lang)
                row_idx += 1
    return b.text, b.blocks, 1


def _pdf_lines(page) -> list[str]:
    """Строки страницы, включая мелкий и светлый текст (pdfplumber не фильтрует по цвету и размеру)."""
    words = page.extract_words(keep_blank_chars=False, use_text_flow=False, x_tolerance=1.5, y_tolerance=2,
                               extra_attrs=['size'])
    if not words:
        return []
    words.sort(key=lambda w: (round(w['top'], 1), w['x0']))
    lines: list[list[dict]] = []
    for w in words:
        mid = (w['top'] + w['bottom']) / 2
        if lines:
            last = lines[-1]
            ref = last[0]
            ref_mid = (ref['top'] + ref['bottom']) / 2
            tol = max(1.0, min(ref['bottom'] - ref['top'], w['bottom'] - w['top']) * 0.5)
            if abs(mid - ref_mid) <= tol:
                last.append(w)
                continue
        lines.append([w])
    out = []
    for ln in lines:
        ln.sort(key=lambda w: w['x0'])
        out.append(_clean_line(' '.join(w['text'] for w in ln)))
    return [o for o in out if o]


def _parse_pdf(data: bytes) -> tuple[str, list[DocBlock], int]:
    import pdfplumber

    b = _Builder()
    with pdfplumber.open(io.BytesIO(data)) as pdf:
        n = len(pdf.pages)
        for pi, page in enumerate(pdf.pages, 1):
            for line in _pdf_lines(page):
                b.add(line, page=pi)
    return b.text, b.blocks, max(n, 1)


def _parse_txt(data: bytes) -> tuple[str, list[DocBlock], int]:
    for enc in ('utf-8-sig', 'cp1251', 'latin-1'):
        try:
            s = data.decode(enc)
            break
        except UnicodeDecodeError:
            continue
    b = _Builder()
    for line in s.splitlines():
        b.add(line.rstrip())
    return b.text, b.blocks, 1


def classify_doc(text: str, name: str) -> DocKind:
    head = text[:600]
    low = head.lower()
    if re.search(r'notice of change|change of bank details|уведомлени[ея] об изменении|письмо', low) \
            or re.search(r'^dear\b', low, re.M):
        return 'letter'
    if re.search(r'\binvoice\s*(no|№|number)|commercial invoice|proforma invoice|^invoice\b|инвойс\s*№|счёт-фактура', low, re.M):
        return 'invoice'
    if re.search(r'^\s*(contract|договор|контракт)\b', low, re.M) or 'hereinafter referred to' in text[:3000].lower():
        return 'contract'
    n = name.lower()
    if any(k in n for k in ('letter', 'notice', 'письмо', 'change')):
        return 'letter'
    if any(k in n for k in ('invoice', 'инвойс', 'inv')):
        return 'invoice'
    if any(k in n for k in ('contract', 'договор', 'контракт')):
        return 'contract'
    return 'other'


def parse_file(data: bytes, name: str, doc_id: str | None = None) -> ParsedDoc:
    ext = name.lower().rsplit('.', 1)[-1] if '.' in name else ''
    if ext == 'docx' or (data[:2] == b'PK' and ext not in ('pdf', 'txt')):
        fmt, (text, blocks, pages) = 'docx', _parse_docx(data)
    elif ext == 'pdf' or data[:5] == b'%PDF-':
        fmt, (text, blocks, pages) = 'pdf', _parse_pdf(data)
    else:
        fmt, (text, blocks, pages) = 'txt', _parse_txt(data)
    return ParsedDoc(id=doc_id or name, name=name.rsplit('/', 1)[-1], kind=classify_doc(text, name),
                     format=fmt, text=text, blocks=blocks, pages=pages)

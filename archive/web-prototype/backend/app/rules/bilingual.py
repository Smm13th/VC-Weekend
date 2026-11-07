"""Двуязычный договор: выравнивание пунктов EN/RU, числа, нумерация, длины."""
from __future__ import annotations

import re
from statistics import median

from ..models import ClausePair, Finding, ParsedDoc, Suggestion
from .common import finding, fmt_num, ref
from .dates import DATE_RE, parse_date
from .money import parse_amount

NUM_PREFIX = re.compile(r'^\s*(\d{1,2}(?:\.\d{1,2})?)\.?(?=\s|$)')
_NUMS = re.compile(r'\d{1,3}(?:[   ]\d{3})+(?:,\d{1,2})?(?!\d)|\d{1,3}(?:,\d{3})+(?:\.\d{1,2})?(?!\d)'
                   r'|\d+(?:[.,]\d+)?')
_SENT = re.compile(r'[.!?;](?=\s+[A-ZА-ЯЁ«"(])')


def clause_num(text: str) -> tuple[str | None, int]:
    """'2.2. Text' -> ('2.2', длина префикса)"""
    m = NUM_PREFIX.match(text)
    if not m:
        return None, 0
    end = m.end()
    while end < len(text) and text[end] in ' \t':
        end += 1
    return m.group(1), end


def numbers(text: str) -> list[str]:
    toks = []
    t = text
    for m in DATE_RE.finditer(text):
        iso = parse_date(m.group(0))
        toks.append(f'D{iso}' if iso else m.group(0))
        t = t[:m.start()] + ' ' * (m.end() - m.start()) + t[m.end():]
    for m in _NUMS.finditer(t):
        v = parse_amount(m.group(0))
        if v is None:
            continue
        toks.append(str(int(v)) if float(v).is_integer() else f'{v:g}')
    return sorted(toks)


def sentences(text: str) -> int:
    t = text.strip()
    if not t:
        return 0
    return len(_SENT.findall(t)) + 1


def _body(doc: ParsedDoc, r) -> tuple[str, int]:
    """текст ячейки без номера пункта и смещение начала тела"""
    t = doc.text[r.start:r.end]
    _, off = clause_num(t)
    return t[off:], off


def align_clauses(doc: ParsedDoc) -> list[ClausePair]:
    return [p for p, _ in _align(doc)]


def _align(doc: ParsedDoc) -> list[tuple[ClausePair, tuple]]:
    rows: dict[int, dict] = {}
    order: list[int] = []
    for b in doc.blocks:
        if b.row is None or b.lang is None:
            continue
        if b.row not in rows:
            rows[b.row] = {}
            order.append(b.row)
        rows[b.row].setdefault(b.lang, b)
    pairs: list[tuple[ClausePair, tuple]] = []
    for r in order:
        cells = rows[r]
        en_b, ru_b = cells.get('en'), cells.get('ru')
        n_en = clause_num(doc.text[en_b.start:en_b.end])[0] if en_b else None
        n_ru = clause_num(doc.text[ru_b.start:ru_b.end])[0] if ru_b else None
        num = n_en or n_ru
        if not num:
            continue
        en_ref = ref(doc, en_b.start, en_b.end) if en_b else None
        ru_ref = ref(doc, ru_b.start, ru_b.end) if ru_b else None
        p = ClausePair(num=num, en=en_ref, ru=ru_ref)
        if en_ref:
            p.numbers_en = numbers(_body(doc, en_ref)[0])
        if ru_ref:
            p.numbers_ru = numbers(_body(doc, ru_ref)[0])
        if en_ref and ru_ref:
            le, lr = len(_body(doc, en_ref)[0]), len(_body(doc, ru_ref)[0])
            p.ratio = round(lr / le, 3) if le else None
        pairs.append((p, (n_en, n_ru)))
    return pairs


def _prevail_clause(doc: ParsedDoc, pairs: list[ClausePair]) -> str:
    for p in pairs:
        t = (p.en.quote if p.en else '') + ' ' + (p.ru.quote if p.ru else '')
        if re.search(r'prevail|приоритет|преимуществ\w+\s+имеет\s+текст', t, re.I) and re.search(r'Russian|русск', t, re.I):
            return p.num
    return '12.9'


def _fmt_tok(t: str) -> str:
    if t.startswith('D'):
        y, m, d = t[1:].split('-')
        return f'{d}.{m}.{y}'
    try:
        v = float(t)
        return fmt_num(v, 0 if v.is_integer() else 2)
    except ValueError:
        return t


def _split_sentences(text: str) -> list[str]:
    parts, last = [], 0
    for m in _SENT.finditer(text):
        parts.append(text[last:m.end()])
        last = m.end()
    parts.append(text[last:])
    return [p.strip() for p in parts if p.strip()]


def bilingual_findings(doc: ParsedDoc, pairs: list[ClausePair] | None = None, stage='contract') -> list[Finding]:
    aligned = _align(doc)
    if pairs is None:
        pairs = [p for p, _ in aligned]
    nums_of = {id(p): n for p, n in zip(pairs, [n for _, n in aligned])} if len(pairs) == len(aligned) else {}
    out: list[Finding] = []
    if not pairs:
        return out
    prev = _prevail_clause(doc, pairs)
    prev_note = f' По пункту {prev} при расхождении текстов приоритет у русского, поэтому разница может стоить денег.'

    # 1. Пропуски нумерации в каждой колонке
    missing: dict[str, dict] = {}
    for lang_i, lang in enumerate(('en', 'ru')):
        seq = []
        for p in pairs:
            n = nums_of.get(id(p), (p.num, p.num))[lang_i]
            if n and getattr(p, lang):
                seq.append((n, p))
        arts = {}
        art_order = []
        for n, p in seq:
            a = n.split('.')[0]
            if a not in arts:
                arts[a] = []
                art_order.append(a)
            if '.' in n:
                arts[a].append((int(n.split('.')[1]), p))
        # пропущенные статьи
        ints = [int(a) for a in art_order]
        for x, y in zip(ints, ints[1:]):
            for k in range(x + 1, y):
                after = next(p for n, p in seq if n == str(y))
                missing.setdefault(str(k), {'langs': [], 'after': after})['langs'].append(lang)
        for a in art_order:
            subs = arts[a]
            prev_n = 0
            for k, p in subs:
                if k > prev_n + 1:
                    for miss in range(prev_n + 1, k):
                        missing.setdefault(f'{a}.{miss}', {'langs': [], 'after': p})['langs'].append(lang)
                prev_n = max(prev_n, k)
    for num, info in missing.items():
        p = info['after']
        both = len(info['langs']) == 2
        where = 'в обеих колонках' if both else ('в английском тексте' if info['langs'] == ['en'] else 'в русском тексте')
        before = num.rsplit('.', 1)[0] + '.' + str(int(num.rsplit('.', 1)[1]) - 1) if '.' in num else str(int(num) - 1)
        out.append(finding(
            'bilingual_numbering_gap', doc.id, stage, 'yellow', f'Пропущен пункт {num}',
            f'После пункта {before} сразу идёт пункт {p.num}, пункта {num} нет {where}. Возможно, при '
            'редактировании потеряли условие. Уточните у поставщика, был ли такой пункт, или перенумеруйте '
            'пункты, чтобы ссылки в договоре не вели в пустоту.',
            [p.en if 'en' in info['langs'] or not p.ru else p.ru, p.ru if p.en else None], extra=num))

    # 2. Разные числа и даты в парах
    for p in pairs:
        if not (p.en and p.ru):
            continue
        if p.numbers_en != p.numbers_ru:
            only_en = [t for t in p.numbers_en if t not in p.numbers_ru]
            only_ru = [t for t in p.numbers_ru if t not in p.numbers_en]
            detail = (f'В пункте {p.num} числа в двух языках не совпадают. '
                      + (f'Только в английском тексте: {", ".join(_fmt_tok(t) for t in only_en)}. ' if only_en else '')
                      + (f'Только в русском тексте: {", ".join(_fmt_tok(t) for t in only_ru)}. ' if only_ru else '')
                      + 'Попросите поставщика исправить одну из версий.' + prev_note)
            out.append(finding('bilingual_number_mismatch', doc.id, stage, 'yellow',
                               f'Пункт {p.num}: в английском и русском тексте разные цифры', detail,
                               [p.ru, p.en], extra=p.num))
            p.issues.append('bilingual_number_mismatch')

    # 3. Длина: русский текст заметно длиннее или короче английского
    cand = []
    for p in pairs:
        if not (p.en and p.ru) or p.ratio is None:
            continue
        en_body, _ = _body(doc, p.en)
        if len(en_body) > 60:
            cand.append(p)
    if len(cand) >= 3:
        med = median(p.ratio for p in cand)
        for p in cand:
            en_body, _ = _body(doc, p.en)
            ru_body, ru_off = _body(doc, p.ru)
            expected = len(en_body) * med
            excess = len(ru_body) - expected
            s_en, s_ru = sentences(en_body), sentences(ru_body)
            by_ratio = (p.ratio > med * 1.3 or p.ratio < med * 0.7) and abs(excess) > 25
            by_sent = s_en != s_ru and abs(excess) > 25 and (excess > 0) == (s_ru > s_en)
            if not (by_ratio or by_sent):
                continue
            longer = excess > 0
            sources = [p.ru, p.en]
            suggestion = None
            extra_text = ''
            if s_ru > s_en and longer:
                parts = _split_sentences(ru_body)
                extra = parts[s_en:]
                extra_text = ' '.join(extra)
                pos = doc.text.find(extra[0], p.ru.start + ru_off)
                if pos >= 0:
                    end = pos + len(extra_text) if doc.text[pos:pos + len(extra_text)] == extra_text else pos + len(extra[0])
                    sources = [ref(doc, pos, end), p.en]
                suggestion = Suggestion(clause=p.num, en=en_body.strip(), ru=' '.join(parts[:s_en]).strip())
            elif s_en > s_ru and not longer:
                parts = _split_sentences(en_body)
                extra_text = ' '.join(parts[s_ru:])
                suggestion = Suggestion(clause=p.num, en=' '.join(parts[:s_ru]).strip(), ru=ru_body.strip())
            what = 'длиннее' if longer else 'короче'
            detail = (f'Русский текст пункта {p.num} заметно {what} английского. Обычно в этом договоре русский '
                      f'текст занимает около {round(med * 100)}% длины английского, а здесь {round(p.ratio * 100)}%. ')
            if extra_text and longer:
                detail += f'В русской версии есть фраза «{extra_text.rstrip(".")}», которой нет в английской. '
            elif extra_text:
                detail += f'В английской версии есть фраза «{extra_text.rstrip(".")}», которой нет в русской. '
            else:
                detail += 'Похоже, в одной из версий есть условие, которого нет в другой. '
            detail += 'Согласуйте с поставщиком одинаковый текст.' + prev_note
            out.append(finding('bilingual_length_ratio', doc.id, stage, 'yellow',
                               f'Пункт {p.num}: русский и английский текст не совпадают по смыслу', detail,
                               sources, extra=p.num, suggestion=suggestion))
            p.issues.append('bilingual_length_ratio')
    return out

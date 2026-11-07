"""Назначение платежа: латиница и длина, шаблон."""
from __future__ import annotations

import re

from .dates import parse_date

ALLOWED = re.compile(r"[A-Za-z0-9 /\-?:().,'+]")
MAX_LEN = 140

_TR = {'а': 'a', 'б': 'b', 'в': 'v', 'г': 'g', 'д': 'd', 'е': 'e', 'ё': 'e', 'ж': 'zh', 'з': 'z', 'и': 'i',
       'й': 'y', 'к': 'k', 'л': 'l', 'м': 'm', 'н': 'n', 'о': 'o', 'п': 'p', 'р': 'r', 'с': 's', 'т': 't',
       'у': 'u', 'ф': 'f', 'х': 'kh', 'ц': 'ts', 'ч': 'ch', 'ш': 'sh', 'щ': 'shch', 'ъ': '', 'ы': 'y', 'ь': '',
       'э': 'e', 'ю': 'yu', 'я': 'ya', 'қ': 'k', 'ғ': 'g', 'ү': 'u', 'ұ': 'u', 'ң': 'n', 'ө': 'o', 'һ': 'h',
       'і': 'i', 'ә': 'a', '№': 'NO', '«': '', '»': '', '"': '', '_': ' ', '&': ' AND ', ';': ',', '#': 'NO '}


def translit(s: str) -> str:
    out = []
    for ch in s:
        low = ch.lower()
        if low in _TR:
            t = _TR[low]
            out.append(t.upper() if ch.isupper() else t)
        else:
            out.append(ch)
    return ''.join(out)


def sanitize(s: str) -> str:
    s = translit(s)
    s = ''.join(ch if ALLOWED.match(ch) else ' ' for ch in s)
    return re.sub(r'\s+', ' ', s).strip()


def validate_purpose(text: str | None) -> dict:
    text = text or ''
    issues = []
    bad = [i for i, ch in enumerate(text) if not ALLOWED.match(ch)]
    if bad:
        chars = ''.join(dict.fromkeys(text[i] for i in bad))
        issues.append({'rule': 'purpose_latin',
                       'message': f'Недопустимые символы: {" ".join(repr(c) for c in chars)}. Разрешены латинские '
                                  "буквы, цифры, пробел и знаки / - ? : ( ) . , ' +",
                       'positions': bad})
    if len(text) > MAX_LEN:
        issues.append({'rule': 'purpose_length',
                       'message': f'Назначение длиннее {MAX_LEN} символов: {len(text)}. Лишнее банк обрежет.',
                       'positions': list(range(MAX_LEN, len(text)))})
    if not text.strip():
        issues.append({'rule': 'purpose_length', 'message': 'Назначение перевода не заполнено.', 'positions': []})
    return {'ok': not issues, 'length': len(text), 'issues': issues}


def _ddmm(d) -> str:
    iso = parse_date(d)
    if not iso:
        return sanitize(str(d or ''))
    y, m, dd = iso.split('-')
    return f'{dd}.{m}.{y}'


def build_purpose_template(invoice_no, invoice_date, contract_no, contract_date, goods=None,
                           accounting_number=None, prepay=False) -> str:
    head = 'PREPAYMENT FOR' if prepay else 'PAYMENT FOR'
    goods_s = sanitize(goods or 'GOODS').upper()
    parts_tail = []
    if invoice_no:
        parts_tail.append(f'INV {sanitize(invoice_no).upper()}' + (f' DD {_ddmm(invoice_date)}' if invoice_date else ''))
    if contract_no:
        parts_tail.append(f'CONTRACT {sanitize(contract_no).upper()}'
                          + (f' DD {_ddmm(contract_date)}' if contract_date else ''))
    tail = ' '.join(parts_tail)
    un = f'UN {sanitize(accounting_number).upper()}' if accounting_number else ''

    def compose(g, with_un):
        s = ' '.join(x for x in (f'{head} {g}'.strip(), tail, un if with_un else '') if x)
        return re.sub(r'\s+', ' ', s).strip()

    for with_un in (True, False) if un else (False,):
        s = compose(goods_s, with_un)
        if len(s) <= MAX_LEN:
            return s
        # укорачиваем описание товара по словам
        words = goods_s.split()
        while len(words) > 1:
            words.pop()
            s = compose(' '.join(words), with_un)
            if len(s) <= MAX_LEN:
                return s
        s = compose('GOODS', with_un)
        if len(s) <= MAX_LEN:
            return s
    return compose('GOODS', False)[:MAX_LEN].rstrip()

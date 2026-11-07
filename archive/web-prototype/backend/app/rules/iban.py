"""IBAN: формат, длина по стране, контрольная сумма mod 97, подсказки по опечаткам."""
from __future__ import annotations

import re
import string

from .. import refdata

_ALNUM = string.digits + string.ascii_uppercase


def normalize_iban(s: str | None) -> str:
    return re.sub(r'[\s  \-]', '', s or '').upper()


def format_iban(s: str) -> str:
    n = normalize_iban(s)
    return ' '.join(n[i:i + 4] for i in range(0, len(n), 4))


def mask_iban(s: str) -> str:
    n = normalize_iban(s)
    if len(n) < 8:
        return n
    return f'{n[:4]} **** {n[-4:]}'


def mod97_ok(n: str) -> bool:
    r = n[4:] + n[:4]
    try:
        return int(''.join(str(int(c, 36)) for c in r)) % 97 == 1
    except ValueError:
        return False


def country_info(code: str) -> dict | None:
    for c in refdata.load('countries'):
        if c['code'] == code:
            return c
    return None


def _diff_hint(n: str, refn: str, label: str, ref_label: str) -> str | None:
    if len(n) != len(refn) or n == refn:
        return None
    diff = [i for i in range(len(n)) if n[i] != refn[i]]
    if not diff:
        return None
    a, b = diff[0], diff[-1]
    here, there = n[a:b + 1], refn[a:b + 1]
    pos = f'{a + 1}' if a == b else f'{a + 1}-{b + 1}'
    word = 'символ' if a == b else 'символы'
    swap = len(diff) == 2 and b == a + 1 and n[a] == refn[b] and n[b] == refn[a]
    tail = ', похоже на перестановку соседних символов' if swap else ''
    return f'{label} {pos} {word} "{here}", {ref_label} "{there}"{tail}'


def typo_candidates(n: str, limit: int = 3) -> list[str]:
    """Перестановки соседних символов и одиночные замены, дающие верную контрольную сумму."""
    out: list[str] = []

    def add(c):
        if c != n and c not in out and mod97_ok(c):
            out.append(c)

    for i in range(4, len(n) - 1):
        if n[i] != n[i + 1]:
            add(n[:i] + n[i + 1] + n[i] + n[i + 2:])
            if len(out) >= limit:
                return out
    for i in list(range(4, len(n))) + [2, 3]:
        alphabet = string.digits if n[i].isdigit() else _ALNUM
        for ch in alphabet:
            if ch != n[i]:
                add(n[:i] + ch + n[i + 1:])
                if len(out) >= limit:
                    return out
    return out


def validate_iban(s: str | None, reference: str | None = None, label: str = 'в инвойсе',
                  ref_label: str = 'в договоре') -> dict:
    n = normalize_iban(s)
    res = {'ok': False, 'normalized': n, 'country': n[:2] if len(n) >= 2 else None, 'error': None,
           'hint': None, 'candidates': []}
    if not re.fullmatch(r'[A-Z]{2}\d{2}[A-Z0-9]{8,30}', n):
        res['error'] = 'format'
        res['hint'] = ('IBAN начинается с двух латинских букв кода страны и двух цифр, дальше только '
                       'латинские буквы и цифры, всего от 12 до 34 символов')
        return res
    info = country_info(n[:2])
    if info is not None:
        if info.get('ibanLength') is None:
            res['error'] = 'format'
            res['hint'] = f'в стране {info["name"]} IBAN не используется, нужен номер счёта и код банка'
            return res
        if len(n) != info['ibanLength']:
            res['error'] = 'length'
            res['hint'] = (f'для страны {info["name"]} IBAN состоит из {info["ibanLength"]} символов, '
                           f'а здесь {len(n)}')
            return res
    if not mod97_ok(n):
        res['error'] = 'checksum'
        refn = normalize_iban(reference) if reference else ''
        hint = _diff_hint(n, refn, label, ref_label) if refn and mod97_ok(refn) else None
        if hint:
            res['hint'] = hint
            res['candidates'] = [refn]
        else:
            res['candidates'] = typo_candidates(n)
            res['hint'] = ('контрольные цифры не сходятся, в номере опечатка'
                           + (f'; возможно, имелось в виду {", ".join(format_iban(c) for c in res["candidates"])}'
                              if res['candidates'] else ''))
        return res
    res['ok'] = True
    return res


def looks_like_typo(a: str, b: str) -> bool:
    """a и b отличаются в 1-2 символах или перестановкой соседних."""
    a, b = normalize_iban(a), normalize_iban(b)
    if len(a) != len(b) or a == b:
        return False
    diff = [i for i in range(len(a)) if a[i] != b[i]]
    if len(diff) <= 2:
        return True
    return False

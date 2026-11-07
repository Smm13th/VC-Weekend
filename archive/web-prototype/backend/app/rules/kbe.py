"""Код бенефициара (КБе): резидентство + сектор экономики."""
from __future__ import annotations

import re

from .. import refdata
from ..models import KbeInfo

_BANK = re.compile(r'\b(bank|banque|banca|банк)\b', re.I)
_GOV = re.compile(r'\b(ministry|government|министерств|акимат|казначейств|treasury)', re.I)
_FIN = re.compile(r'\b(insurance|leasing|investment fund|страхов|лизинг|microfinance|микрофинанс)', re.I)
_NPO = re.compile(r'\b(foundation|charity|non-profit|фонд|благотворител)', re.I)
_LEGAL = re.compile(r'\b(LLC|LLP|FZE|FZCO|FZ-LLC|LTD|LIMITED|JSC|PJSC|INC|CORP|GMBH|AG|SA|SRL|BV|PLC|CO|ТОО|АО|ООО|ЗАО|ПАО|ИП)\b'
                    r'|company|trading|machinery|industries', re.I)


def suggest_kbe(country: str | None, name: str | None) -> KbeInfo:
    country = (country or '').upper()
    name = name or ''
    data = refdata.load('kbe')
    res = '1' if country == 'KZ' else '2'
    if _BANK.search(name):
        sec = '4'
    elif _GOV.search(name):
        sec = '1'
    elif _FIN.search(name):
        sec = '5'
    elif _NPO.search(name):
        sec = '8'
    elif _LEGAL.search(name) or not name:
        sec = '7'
    else:
        sec = '9'
    res_title = next(r['title'] for r in data['residency'] if r['digit'] == res)
    sec_title = next(s['title'] for s in data['sectors'] if s['digit'] == sec)
    cname = country
    for c in refdata.load('countries'):
        if c['code'] == country:
            cname = c['name']
    where = f'зарегистрирован в стране {cname}' if country else 'страна не определена'
    why = (f'Получатель {where}, поэтому первая цифра {res}: {res_title.lower()}. '
           f'Вторая цифра {sec}: {sec_title.lower()}'
           + (f', судя по названию «{name}».' if name else '.')
           + ' Сверьте код со справочником Национального Банка РК.')
    return KbeInfo(code=res + sec, why=why, mentor_check=True)

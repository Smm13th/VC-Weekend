"""Проверки одного документа: плейсхолдеры, скрытые инструкции, IBAN и SWIFT."""
from __future__ import annotations

import re

from .. import refdata
from ..models import ExtractionResult, Finding, ParsedDoc
from .common import PLACEHOLDER, doc_label, finding, ref
from .iban import format_iban, validate_iban
from .injection import detect_injections
from .swift import validate_swift


def placeholder_finding(doc: ParsedDoc, stage: str) -> Finding | None:
    hits = [ref(doc, m.start(), m.end()) for m in PLACEHOLDER.finditer(doc.text)]
    if not hits:
        return None
    names = list(dict.fromkeys(h.quote for h in hits))
    shown = ', '.join(names[:8]) + (f' и ещё {len(names) - 8}' if len(names) > 8 else '')
    return finding('placeholder', doc.id, stage, 'red', f'В {doc_label(doc)} не заполнены поля шаблона',
                   f'В тексте остались заготовки в квадратных скобках ({len(hits)} шт.): {shown}. Это шаблон, а не '
                   'подписанный документ. Платить по нему нельзя: попросите у поставщика заполненную и подписанную '
                   'версию с номером, датой, сторонами и банковскими реквизитами.', hits)


def injection_finding(doc: ParsedDoc, stage: str) -> Finding | None:
    hits = detect_injections(doc)
    if not hits:
        return None
    return finding('hidden_instruction', doc.id, stage, 'red', f'В {doc_label(doc)} спрятана инструкция для помощника',
                   f'В документе есть текст, обращённый к программе-помощнику, а не к людям: «{hits[0].quote}». '
                   'Часто его набирают мелким или светлым шрифтом, чтобы человек не заметил. Помощник такие '
                   'указания не выполняет и ничего по ним не подтверждает. Такая строка уже повод насторожиться. '
                   'Покажите документ поставщику и службе безопасности банка, прежде чем платить.', hits)


def _country_name(code: str | None) -> str:
    for c in refdata.load('countries'):
        if c['code'] == code:
            return c['name']
    return code or '?'


def requisites_findings(doc: ParsedDoc, ext: ExtractionResult, stage: str,
                        reference_iban: str | None = None) -> list[Finding]:
    out: list[Finding] = []
    lbl = doc_label(doc)
    ib = ext.fields.get('sellerIban')
    sw = ext.fields.get('sellerSwift')
    iban_res = None
    if ib:
        iban_res = validate_iban(ib.value, reference=reference_iban if doc.kind != 'contract' else None,
                                 label=f'в {lbl}', ref_label='в договоре')
        if iban_res['error'] in ('format', 'length'):
            out.append(finding('iban_format', doc.id, stage, 'red', f'IBAN в {lbl} записан неправильно',
                               f'Номер счёта получателя {ib.value} не похож на правильный IBAN: {iban_res["hint"]}. '
                               'Перевод с таким номером банк не примет или вернёт. Попросите у поставщика верный номер.',
                               [ib.source], field='beneficiaryIban'))
        elif iban_res['error'] == 'checksum':
            cand = iban_res['candidates']
            tip = (f' Похоже на опечатку: {iban_res["hint"]}.' if iban_res['hint'] else '')
            if cand and reference_iban and doc.kind != 'contract':
                tip += f' Правильный вариант, скорее всего, {format_iban(cand[0])}, но подтвердите его у поставщика.'
            out.append(finding('iban_checksum', doc.id, stage, 'red', f'В IBAN в {lbl} опечатка',
                               f'У номера {ib.value} не сходятся контрольные цифры: такого счёта не существует, '
                               f'перевод вернётся, а банк возьмёт комиссию.{tip}', [ib.source], field='beneficiaryIban'))
        else:
            out.append(finding('iban_checksum', doc.id, stage, 'green', f'IBAN в {lbl} проходит проверку',
                               f'Номер {format_iban(ib.value)} правильной длины для страны '
                               f'{_country_name(iban_res["country"])}, контрольные цифры сходятся.',
                               [ib.source], field='beneficiaryIban'))
    if sw:
        sres = validate_swift(sw.value)
        if not sres['ok']:
            out.append(finding('swift_format', doc.id, stage, 'red', f'SWIFT в {lbl} записан неправильно',
                               f'Код банка {sw.value} не похож на SWIFT: нужно 8 или 11 символов, сначала 4 буквы банка, '
                               '2 буквы страны, потом 2 символа города и, если есть, 3 символа филиала.',
                               [sw.source], field='beneficiarySwift'))
        else:
            if iban_res and iban_res['ok'] and sres['country'] != iban_res['country']:
                out.append(finding('swift_iban_country', doc.id, stage, 'red', f'SWIFT и IBAN в {lbl} из разных стран',
                                   f'По коду SWIFT {sres["normalized"]} банк находится в стране '
                                   f'{_country_name(sres["country"])}, а IBAN открыт в стране '
                                   f'{_country_name(iban_res["country"])}. Так не бывает для одного счёта: '
                                   'уточните реквизиты у поставщика.', [sw.source, ib.source], field='beneficiarySwift'))
            if sres['bank']:
                b = sres['bank']
                out.append(finding('swift_format', doc.id, stage, 'green', f'SWIFT в {lbl} найден в справочнике',
                                   f'Код {sres["normalized"]} принадлежит банку {b["name"]}, {b["city"]}.',
                                   [sw.source], field='beneficiarySwift'))
    inter = ext.fields.get('intermediaryBank')
    if inter:
        m = re.search(r'\b[A-Z0-9]{8,11}\b', inter.value.split('SWIFT')[-1]) if 'SWIFT' in inter.value.upper() else None
        if m and not validate_swift(m.group(0))['ok']:
            out.append(finding('swift_format', doc.id, stage, 'red', f'SWIFT банка-посредника в {lbl} неправильный',
                               f'Код {m.group(0)} не похож на SWIFT. Уточните банк-посредник у поставщика.',
                               [inter.source], field='intermediaryBank', extra='intermediary'))
    return out

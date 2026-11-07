"""Проверка договора: дефекты пунктов, двуязычие, учётный номер."""
from __future__ import annotations

import re

from .. import refdata
from ..extract import extract_fields
from ..models import ClausePair, ContractReport, ContractSummary, ExtractionResult, Finding, ParsedDoc, Suggestion
from .balance import contract_balance
from .bilingual import _body, align_clauses, bilingual_findings
from .common import finding, fmt_date, fmt_money, ref
from .docs import injection_finding, placeholder_finding, requisites_findings
from .money import convert, rate

FEES_EN = "Bank charges of the Buyer's bank shall be borne by the Buyer; all other bank charges shall be borne by the Seller."
FEES_RU = 'Комиссии банка Покупателя оплачивает Покупатель, все прочие банковские комиссии оплачивает Продавец.'
TERM_EN = 'The Buyer shall pay each Invoice within 10 (ten) banking days from the date of the Invoice.'
TERM_RU = 'Покупатель оплачивает каждый Инвойс в течение 10 (десяти) банковских дней с даты Инвойса.'


def _text(p: ClausePair, lang: str) -> str:
    r = getattr(p, lang)
    return r.quote if r else ''


def _span_ref(doc: ParsedDoc, r, m) -> object:
    return ref(doc, r.start + m.start(), r.start + m.end())


def threshold_usd() -> float:
    return float(refdata.tariffs().get('accountingNumberThresholdUsd', 50000))


def contract_summary(ext: ExtractionResult | None, payments=None) -> ContractSummary:
    s = ContractSummary(threshold_usd=threshold_usd())
    if not ext:
        return s
    f = ext.fields
    s.contract_no = f['contractNo'].value if 'contractNo' in f else None
    s.contract_date = f['contractDate'].norm if 'contractDate' in f else None
    s.currency = f['currency'].norm if 'currency' in f else None
    if 'totalAmount' in f and isinstance(f['totalAmount'].norm, float):
        s.total = f['totalAmount'].norm
        try:
            s.total_usd = convert(s.total, s.currency or 'USD', 'USD')
        except KeyError:
            s.total_usd = None
    s.paid, s.remaining = contract_balance(s.total, payments or [], s.currency)
    s.needs_accounting_number = bool(s.total_usd and s.total_usd > s.threshold_usd)
    return s


def conversion_note(s: ContractSummary) -> str:
    if not s.total or not s.currency:
        return ''
    if s.currency == 'USD':
        return f'Сумма договора {fmt_money(s.total, "USD")} больше порога {fmt_money(s.threshold_usd, "USD", 0)}.'
    r = rate(s.currency) / rate('USD')
    return (f'Договор в другой валюте, поэтому сумму пересчитали: {fmt_money(s.total, s.currency, 0)} это примерно '
            f'{fmt_money(s.total_usd, "USD", 0)} по курсу {fmt_money(r, "", 4).strip()} USD за 1 {s.currency} '
            f'(через тенге, курсы на {fmt_date(refdata.load("rates").get("date"))}). Это больше порога '
            f'{fmt_money(s.threshold_usd, "USD", 0)}.')


def acc_number_contract_finding(doc: ParsedDoc, ext: ExtractionResult, s: ContractSummary) -> Finding | None:
    if not s.needs_accounting_number:
        return None
    src = ext.fields['totalAmount'].source if 'totalAmount' in ext.fields else None
    return finding('acc_number_required', doc.id, 'contract', 'yellow', 'До первого платежа нужно получить учётный номер',
                   f'{conversion_note(s)} Такой валютный договор нужно поставить на учёт в банке и получить учётный '
                   'номер до первого платежа, иначе перевод не пропустит валютный контроль. Порог и правила '
                   'стоит сверить с менторами.', [src], field='linkContract', mentor_check=True)


def clause_findings(doc: ParsedDoc, pairs: list[ClausePair]) -> list[Finding]:
    out: list[Finding] = []
    # 8.2: согласие не той стороны
    rx_en = re.compile(r"\bThe\s+(?P<p>Seller|Buyer)\s+shall\s+not\b[^.;]*?\bwithout\s+(?:the\s+)?(?:prior\s+)?(?:written\s+)?"
                       r"(?:consent\s+of\s+the\s+(?P=p)|(?P=p)'s\s+(?:prior\s+)?(?:written\s+)?consent)", re.I)
    rx_ru = re.compile(r'(?P<p>Продав|Покупат)\w*\s+не\s+[^.;]*?без\s+[^.;]*?согласия\s+(?P=p)\w*', re.I)
    for p in pairs:
        me = rx_en.search(_text(p, 'en'))
        mr = rx_ru.search(_text(p, 'ru'))
        if not (me or mr):
            continue
        party = (me.group('p') if me else ('Seller' if mr.group('p').lower().startswith('продав') else 'Buyer')).capitalize()
        other = 'Buyer' if party == 'Seller' else 'Seller'
        ru_party, ru_other = ('Продавца', 'Покупателя') if party == 'Seller' else ('Покупателя', 'Продавца')
        en_t = _body(doc, p.en)[0] if p.en else ''
        ru_t = _body(doc, p.ru)[0] if p.ru else ''
        en_fix = re.sub(rf"\b{party}'s(\s+(?:prior\s+)?(?:written\s+)?consent)", rf"{other}'s\1", en_t)
        en_fix = re.sub(rf'(consent\s+of\s+the\s+){party}', rf'\1{other}', en_fix)
        ru_fix = re.sub(rf'(согласия\s+){ru_party}', rf'\1{ru_other}', ru_t)
        srcs = []
        if me:
            sub = re.search(r"without[^.;]*consent(?:\s+of\s+the\s+\w+)?", me.group(0))
            srcs.append(ref(doc, p.en.start + me.start() + sub.start(), p.en.start + me.start() + sub.end())
                        if sub else _span_ref(doc, p.en, me))
        if mr:
            sub = re.search(r'согласия\s+\w+', mr.group(0))
            srcs.append(ref(doc, p.ru.start + mr.start() + sub.start(), p.ru.start + mr.start() + sub.end())
                        if sub else _span_ref(doc, p.ru, mr))
        out.append(finding('clause_consent_party', doc.id, 'contract', 'yellow',
                           f'Пункт {p.num}: согласие не той стороны',
                           f'По пункту {p.num} {"Продавец" if party == "Seller" else "Покупатель"} может раскрыть '
                           f'чужую информацию, если сам на это согласится. Такое условие ничего не защищает. '
                           f'Согласие должна давать сторона, чья это информация, то есть {ru_other.lower()}.',
                           srcs, extra=p.num, suggestion=Suggestion(clause=p.num, en=en_fix, ru=ru_fix)))
        p.issues.append('clause_consent_party')
    # авиадоставка + коносамент
    for p in pairs:
        t_en, t_ru = _text(p, 'en'), _text(p, 'ru')
        air = re.search(r'\bby\s+air\b|\bair\s+(delivery|transport|freight)', t_en, re.I) or re.search(r'авиа', t_ru, re.I)
        bl_en = re.search(r'Bill\s+of\s+Lading', t_en, re.I)
        bl_ru = re.search(r'коносамент\w*(?:\s*\(Bill\s+of\s+Lading\))?', t_ru, re.I)
        if not (air and (bl_en or bl_ru)):
            continue
        en_t = _body(doc, p.en)[0] if p.en else ''
        ru_t = _body(doc, p.ru)[0] if p.ru else ''
        out.append(finding('clause_air_waybill', doc.id, 'contract', 'yellow',
                           f'Пункт {p.num}: для авиадоставки указан морской документ',
                           f'В пункте {p.num} датой авиадоставки считается дата коносамента (Bill of Lading). '
                           'Коносамент выдают при морской перевозке, для самолёта оформляют авиагрузовую накладную '
                           '(Air Waybill). С таким пунктом сложно доказать дату поставки, а она нужна для валютного '
                           'контроля и гарантии.',
                           [_span_ref(doc, p.en, bl_en) if bl_en else None, _span_ref(doc, p.ru, bl_ru) if bl_ru else None],
                           extra=p.num, suggestion=Suggestion(
                               clause=p.num, en=re.sub(r'Bill\s+of\s+Lading', 'Air Waybill', en_t),
                               ru=re.sub(r'коносамента\s*\(Bill\s+of\s+Lading\)', 'авиагрузовой накладной (Air Waybill)', ru_t))))
        p.issues.append('clause_air_waybill')
    # комиссии
    fee_rx = re.compile(r'bank\s+(charges|commissions?|fees)|комисси', re.I)
    if pairs and not any(fee_rx.search(_text(p, 'en')) or fee_rx.search(_text(p, 'ru')) for p in pairs):
        art4 = [p for p in pairs if p.num.startswith('4.')]
        nxt = f'4.{max(int(p.num.split(".")[1]) for p in art4) + 1}' if art4 else '4.7'
        anchor = art4[-1] if art4 else (pairs[0] if pairs else None)
        out.append(finding('fees_not_specified', doc.id, 'contract', 'yellow', 'Не сказано, кто платит банковские комиссии',
                           'В договоре нет пункта о банковских комиссиях. Если при переводе выбрать тип комиссии SHA '
                           'или BEN, банки по дороге удержат часть денег и продавец получит меньше суммы инвойса. '
                           'Формально это недоплата, и продавец может потребовать доплатить. Безопаснее добавить '
                           f'пункт {nxt} или до договорённости платить с типом OUR.',
                           [anchor.en if anchor else None, anchor.ru if anchor else None], field='feeType',
                           suggestion=Suggestion(clause=nxt, en=FEES_EN, ru=FEES_RU)))
    # срок оплаты в статье 4
    art4 = [p for p in pairs if p.num.startswith('4.')]
    term_rx = re.compile(r'(within|not\s+later\s+than|no\s+later\s+than)\s+\d+|в\s+течение\s+\d+|не\s+позднее\s+\d+', re.I)
    pay_rx = re.compile(r'\bpa(y|id|yment)\b|оплат', re.I)
    if art4 and not any(term_rx.search(t) and pay_rx.search(t) for p in art4 for t in (_text(p, 'en'), _text(p, 'ru'))):
        target = next((p for p in art4 if re.search(r'\bshall\s+pay\b|оплачивает', _text(p, 'en') + _text(p, 'ru'))), art4[0])
        out.append(finding('payment_term_missing', doc.id, 'contract', 'yellow', 'В договоре нет точного срока оплаты',
                           f'В статье 4 срок оплаты не назван, пункт {target.num} отсылает к инвойсу. Тогда срок может '
                           'поменяться от инвойса к инвойсу, а без срока трудно понять, когда платёж просрочен и когда '
                           'по нему ждать товар. Попросите записать срок в днях прямо в договоре.',
                           [target.en, target.ru], extra=target.num,
                           suggestion=Suggestion(clause=target.num, en=TERM_EN, ru=TERM_RU)))
        target.issues.append('payment_term_missing')
    return out


def check_contract(doc: ParsedDoc, ext: ExtractionResult | None = None) -> ContractReport:
    ext = ext or extract_fields(doc)
    pairs = align_clauses(doc)
    findings: list[Finding] = []
    for f in (placeholder_finding(doc, 'contract'), injection_finding(doc, 'contract')):
        if f:
            findings.append(f)
    findings += bilingual_findings(doc, pairs)
    findings += clause_findings(doc, pairs)
    s = contract_summary(ext)
    f = acc_number_contract_finding(doc, ext, s)
    if f:
        findings.append(f)
    findings += requisites_findings(doc, ext, 'contract')
    return ContractReport(findings=findings, clauses=pairs, extraction=ext)

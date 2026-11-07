"""Проверка кейса платежа: все документы, сверка с договором, форма, комиссии, остаток."""
from __future__ import annotations

import re

from ..extract import extract_fields
from ..models import CaseInput, ExtractionResult, FeeSim, Finding, ParsedDoc, PaymentReport
from .balance import account_by_id, check_balance
from .common import doc_label, doc_title, finding, fmt_date, fmt_money, norm_name
from .contract import check_contract, contract_summary, conversion_note
from .dates import parse_date, today_iso, value_dates
from .docs import injection_finding, placeholder_finding, requisites_findings
from .fees import simulate_fees
from .iban import format_iban, looks_like_typo, normalize_iban, validate_iban
from .kbe import suggest_kbe
from .money import parse_amount
from .purpose import validate_purpose
from .swift import normalize_swift

SEV_ORDER = {'red': 0, 'yellow': 1, 'green': 2}
STAGE_ORDER = {'contract': 0, 'documents': 1, 'payment': 2}


def _f(ext: ExtractionResult | None, key: str):
    return ext.fields.get(key) if ext else None


def primary_invoice(docs: list[ParsedDoc], exts: dict[str, ExtractionResult]) -> ParsedDoc | None:
    inv = [d for d in docs if d.kind == 'invoice']
    if not inv:
        return None
    return sorted(inv, key=lambda d: (_f(exts[d.id], 'invoiceDate').norm if _f(exts[d.id], 'invoiceDate') else ''))[-1]


def clause_ref(doc: ParsedDoc | None, pattern: str):
    if not doc:
        return None
    from .bilingual import align_clauses
    for p in align_clauses(doc):
        for r in (p.en, p.ru):
            if r and re.search(pattern, r.quote, re.I):
                return r
    return None


def has_autoextension(doc: ParsedDoc | None) -> bool:
    return bool(doc and re.search(r'automatically\s+(extended|renewed|prolonged)|автоматически\s+(продлева|пролонгир)',
                                  doc.text, re.I))


def cross_doc_findings(d: ParsedDoc, ext: ExtractionResult, contract: ParsedDoc | None,
                       c_ext: ExtractionResult | None) -> list[Finding]:
    out: list[Finding] = []
    if not contract or not c_ext:
        return out
    lbl = doc_label(d)
    c_no = _f(c_ext, 'contractNo')
    c_date = _f(c_ext, 'contractDate')
    c_total = _f(c_ext, 'totalAmount')
    c_cur = _f(c_ext, 'currency')
    amt = _f(ext, 'invoiceAmount')
    cur = _f(ext, 'currency')
    if d.kind == 'invoice':
        if cur and c_cur and cur.norm != c_cur.norm:
            out.append(finding('currency_mismatch', d.id, 'documents', 'red', 'Валюта инвойса не совпадает с договором',
                               f'Инвойс выставлен в {cur.norm}, а договор заключён в {c_cur.norm}. Платёж в другой '
                               'валюте не закроет обязательства по договору без дополнительного соглашения. Попросите '
                               'поставщика переделать инвойс или подписать допсоглашение о валюте.',
                               [cur.source, c_cur.source], field='amount'))
        elif amt and c_total and isinstance(amt.norm, float) and isinstance(c_total.norm, float) and amt.norm > c_total.norm:
            out.append(finding('amount_exceeds_contract', d.id, 'documents', 'red', 'Инвойс больше суммы договора',
                               f'В инвойсе {fmt_money(amt.norm, cur.norm if cur else None)}, а весь договор на '
                               f'{fmt_money(c_total.norm, c_cur.norm if c_cur else None)}. Заплатить больше суммы договора '
                               'нельзя без дополнительного соглашения об увеличении суммы.',
                               [amt.source, c_total.source], field='amount'))
        i_date = _f(ext, 'invoiceDate')
        if i_date and c_date and i_date.norm and c_date.norm and i_date.norm < c_date.norm:
            out.append(finding('invoice_before_contract', d.id, 'documents', 'yellow', 'Инвойс выписан раньше договора',
                               f'Инвойс датирован {i_date.value}, а договор подписан {c_date.value}. Банк может спросить, '
                               'на каком основании выставлен счёт до договора. Уточните у поставщика дату или основание.',
                               [i_date.source, c_date.source], field='docDate'))
    # ссылка на договор
    cref = _f(ext, 'invoiceContractRef')
    if d.kind in ('invoice', 'letter') and c_no:
        if not cref:
            out.append(finding('invoice_contract_ref', d.id, 'documents', 'yellow', f'В {lbl} нет ссылки на договор',
                               f'В {lbl} не указано, к какому договору он относится. Без ссылки на договор '
                               f'{c_no.value} от {c_date.value if c_date else "?"} банк может не принять документ как '
                               'основание платежа. Попросите добавить номер и дату договора.', [c_no.source],
                               field='linkContract'))
        else:
            no, _, dt = str(cref.norm or '').partition('|')
            bad_no = norm_name(no) != norm_name(c_no.value)
            bad_dt = bool(dt) and c_date is not None and dt != c_date.norm
            if bad_no or bad_dt:
                what = []
                if bad_no:
                    what.append(f'номер {no} вместо {c_no.value}')
                if bad_dt:
                    what.append(f'дата {fmt_date(dt)} вместо {c_date.value}')
                out.append(finding('invoice_contract_ref', d.id, 'documents', 'yellow',
                                   f'{doc_title(d).capitalize()} ссылается на другой договор',
                                   f'В {lbl} в ссылке на договор указаны {" и ".join(what)}. Возможно, это опечатка '
                                   'или документ от другого договора. Уточните у поставщика и попросите исправить.',
                                   [cref.source, c_no.source], field='linkContract'))
    # смена реквизитов
    ib, c_ib = _f(ext, 'sellerIban'), _f(c_ext, 'sellerIban')
    sw, c_sw = _f(ext, 'sellerSwift'), _f(c_ext, 'sellerSwift')
    iban_diff = bool(ib and c_ib and normalize_iban(ib.value) != normalize_iban(c_ib.value))
    swift_diff = bool(sw and c_sw and normalize_swift(sw.value)[:8] != normalize_swift(c_sw.value)[:8])
    typo = False
    if iban_diff:
        v = validate_iban(ib.value)
        typo = v['error'] == 'checksum' and looks_like_typo(ib.value, c_ib.value)
    if (iban_diff and not typo) or swift_diff:
        bank = _f(ext, 'sellerBank')
        c4 = clause_ref(contract, r'change\s+of\s+bank\s+details|изменени\w+\s+банковских\s+реквизитов')
        c_bank = _f(c_ext, 'sellerBank')
        new_s = []
        if iban_diff:
            new_s.append(f'счёт {format_iban(ib.value)}')
        if bank:
            new_s.append(f'в банке {bank.value}')
        if sw:
            new_s.append(f'(SWIFT {normalize_swift(sw.value)})')
        old_s = []
        if c_ib:
            old_s.append(f'счёт {format_iban(c_ib.value)}')
        if c_bank:
            old_s.append(f'в банке {c_bank.value}')
        if c_sw:
            old_s.append(f'(SWIFT {normalize_swift(c_sw.value)})')
        n46 = '4.6'
        if c4:
            m = re.match(r'\s*(\d+\.\d+)', c4.quote)
            n46 = m.group(1) if m else n46
        out.append(finding('requisites_changed', d.id, 'documents', 'red',
                           f'В {lbl} другие реквизиты продавца',
                           f'В {lbl} указан {" ".join(new_s)}. В статье 13 договора другие реквизиты: {" ".join(old_s)}. '
                           f'По пункту {n46} договора об изменении реквизитов стороны сообщают заранее. Подмена счёта в '
                           'письме или инвойсе встречается часто, так работают мошенники. Перезвоните поставщику по телефону из '
                           'договора, а не из письма, и спросите, меняли ли они банк. Пока не подтвердили, не платите '
                           'на новый счёт.',
                           [ib.source if iban_diff else sw.source, c_ib.source if iban_diff else c_sw.source, c4],
                           field='beneficiaryIban'))
    elif ib and c_ib and not iban_diff and (not sw or not c_sw or not swift_diff):
        out.append(finding('requisites_changed', d.id, 'documents', 'green', f'Реквизиты в {lbl} совпадают с договором',
                           f'IBAN{" и SWIFT" if sw and c_sw else ""} в {lbl} такие же, как в статье 13 договора.',
                           [ib.source, c_ib.source], field='beneficiaryIban'))
    # имя получателя
    nm = _f(ext, 'sellerName')
    c_names = [x for x in (_f(c_ext, 'sellerName'), _f(c_ext, 'sellerAccountName')) if x]
    if nm and c_names and all(norm_name(nm.value) != norm_name(c.value) for c in c_names):
        out.append(finding('beneficiary_name_mismatch', d.id, 'documents', 'red',
                           f'Получатель в {lbl} назван иначе, чем в договоре',
                           f'В {lbl} получатель «{nm.value}», а в договоре продавец «{c_names[0].value}». Банк получателя '
                           'сверяет название с владельцем счёта и может вернуть перевод. Другое название может значить и '
                           'другую компанию. Уточните у поставщика и платите только компании из договора.',
                           [nm.source, c_names[0].source], field='beneficiaryName'))
    return out


def _amount_to_pay(case: CaseInput, inv_ext: ExtractionResult | None) -> float | None:
    if case.form and case.form.get('amount') and case.form['amount'].status == 'user':
        v = parse_amount(case.form['amount'].value)
        if v:
            return v
    a = _f(inv_ext, 'invoiceAmount')
    return a.norm if a and isinstance(a.norm, float) else None


def check_payment(case: CaseInput) -> PaymentReport:
    from ..prefill import build_form

    today = today_iso(case.today)
    docs = case.docs
    exts = {d.id: extract_fields(d) for d in docs}
    contract = next((d for d in docs if d.kind == 'contract'), None)
    c_ext = exts.get(contract.id) if contract else None
    findings: list[Finding] = []
    if contract:
        findings += check_contract(contract, c_ext).findings
    summary = contract_summary(c_ext, case.payments)
    ref_iban = _f(c_ext, 'sellerIban').value if _f(c_ext, 'sellerIban') else None
    for d in docs:
        if d is contract:
            continue
        ext = exts[d.id]
        for f in (placeholder_finding(d, 'documents'), injection_finding(d, 'documents')):
            if f:
                findings.append(f)
        findings += requisites_findings(d, ext, 'documents', ref_iban)
        findings += cross_doc_findings(d, ext, contract, c_ext)
    # у документа с подменой реквизитов зелёные отметки о формате счёта сбивают с толку
    changed = {f.id.split(':', 1)[1] for f in findings if f.rule == 'requisites_changed' and f.severity == 'red'}
    findings = [f for f in findings if not (f.severity == 'green' and f.rule in ('iban_checksum', 'swift_format')
                                            and f.id.split(':', 1)[1] in changed)]

    inv = primary_invoice(docs, exts)
    inv_ext = exts.get(inv.id) if inv else None
    pay_id = inv.id if inv else (contract.id if contract else 'case')
    currency = (_f(inv_ext, 'currency').norm if _f(inv_ext, 'currency') else None) or summary.currency or 'USD'
    amount = _amount_to_pay(case, inv_ext)

    # этап «платёж»: то, что влияет на форму
    if summary.total is not None and amount is not None and summary.remaining is not None and amount > summary.remaining + 0.005:
        a = _f(inv_ext, 'invoiceAmount')
        findings.append(finding('over_remaining', pay_id, 'payment', 'red', 'Сумма больше остатка по договору',
                                f'Договор на {fmt_money(summary.total, summary.currency)}, уже оплачено '
                                f'{fmt_money(summary.paid, summary.currency)}, остаток '
                                f'{fmt_money(summary.remaining, summary.currency)}. К оплате '
                                f'{fmt_money(amount, currency)}, это на {fmt_money(amount - summary.remaining, currency)} '
                                'больше. Такой платёж выйдет за сумму договора: нужно дополнительное соглашение об '
                                'увеличении суммы или оплатить только остаток.',
                                [a.source if a else None, _f(c_ext, 'totalAmount').source if _f(c_ext, 'totalAmount') else None],
                                field='amount'))
    if summary.needs_accounting_number and contract:
        src = _f(c_ext, 'totalAmount').source if _f(c_ext, 'totalAmount') else None
        if not (case.accounting_number or '').strip():
            findings.append(finding('acc_number_required', pay_id, 'payment', 'red', 'Нет учётного номера договора',
                                    f'{conversion_note(summary)} Без учётного номера банк не проведёт платёж по этому '
                                    'договору. Поставьте договор на учёт в банке, получите учётный номер и укажите его в '
                                    'поле «Привязать валютный договор». Порог стоит сверить с менторами.',
                                    [src], field='linkContract', mentor_check=True))
        else:
            findings.append(finding('acc_number_required', pay_id, 'payment', 'green', 'Учётный номер договора указан',
                                    f'Договор стоит на учёте, учётный номер {case.accounting_number}. Он попадёт в '
                                    'привязку договора и в назначение платежа.', [src], field='linkContract',
                                    mentor_check=True))

    extractions = [exts[d.id] for d in docs]
    form = build_form(case, extractions, findings, summary)

    # этап «платёж»: проверки по форме
    vd = parse_date(form['valueDate'].value) or parse_date(case.value_date)
    exp = _f(c_ext, 'expiryDate')
    if exp and exp.norm and vd and exp.norm < vd:
        auto = has_autoextension(contract)
        c31 = clause_ref(contract, r'valid\s+until|действует\s+до')
        if auto:
            findings.append(finding('contract_expired', contract.id, 'payment', 'yellow',
                                    'Срок договора истёк, но он продлевается сам',
                                    f'Договор действует до {exp.value}, а дата валютирования {fmt_date(vd)}. По пункту о '
                                    'сроке договор автоматически продлевается, если ни одна сторона не заявила о '
                                    'расторжении. Уточните у поставщика, что договор не расторгали; банк может попросить '
                                    'подтверждение.', [exp.source, c31], field='valueDate'))
        else:
            findings.append(finding('contract_expired', contract.id, 'payment', 'red', 'Срок договора истёк',
                                    f'Договор действует до {exp.value}, а дата валютирования {fmt_date(vd)}. По истёкшему '
                                    'договору платить нельзя: нужно дополнительное соглашение о продлении.',
                                    [exp.source, c31], field='valueDate'))
    pv = validate_purpose(form['purpose'].value)
    for iss in pv['issues']:
        findings.append(finding(iss['rule'], 'form', 'payment', 'red',
                                'Назначение перевода: недопустимые символы' if iss['rule'] == 'purpose_latin'
                                else 'Назначение перевода слишком длинное' if pv['length'] > 140
                                else 'Назначение перевода не заполнено',
                                iss['message'], [], field='purpose'))
    fib = form['beneficiaryIban'].value
    if fib:
        v = validate_iban(fib, reference=ref_iban, label='в форме', ref_label='в договоре')
        if v['error']:
            rule = 'iban_checksum' if v['error'] == 'checksum' else 'iban_format'
            findings.append(finding(rule, 'form', 'payment', 'red', 'IBAN получателя в форме не проходит проверку',
                                    f'Номер {fib} в поле «Счёт получателя» неверный: {v["hint"]}. Перевод с ним вернётся.',
                                    [], field='beneficiaryIban'))

    urgent = form['urgent'].value == 'true'
    pay_amount = parse_amount(form['amount'].value) or amount or 0.0
    fees: dict[str, FeeSim] = {t: simulate_fees(pay_amount, currency, t, urgent) for t in ('OUR', 'SHA', 'BEN')}
    fee_type = form['feeType'].value if form['feeType'].value in fees else 'OUR'
    debit = form['debitAccount'].value or None
    balance = None
    if pay_amount:
        balance = check_balance(debit, pay_amount, fees[fee_type].sender_fees, currency)
        if not balance.ok:
            acc = account_by_id(debit)
            if acc is None:
                head = (f'У вас нет счёта в {currency}, а платить нужно {fmt_money(pay_amount, currency)} плюс '
                        f'комиссия {fmt_money(fees[fee_type].sender_fees, currency)}. ')
            else:
                head = (f'На счёте «{acc["title"]}» {fmt_money(balance.available, balance.currency)}, а нужно '
                        f'{fmt_money(balance.needed, balance.currency)} вместе с комиссией {fee_type}. Не хватает '
                        f'{fmt_money(balance.shortfall, balance.currency)}. ')
            opts = ' '.join(f'Вариант: {o.title[:1].lower() + o.title[1:]}. {o.detail}' for o in balance.options)
            findings.append(finding('insufficient_balance', pay_id, 'payment', 'red', 'Не хватает денег на счёте',
                                    head + opts, [], field='debitAccount'))

    # дедуп по id
    seen, uniq = set(), []
    for f in findings:
        if f.id in seen:
            continue
        seen.add(f.id)
        uniq.append(f)
    uniq.sort(key=lambda f: (STAGE_ORDER[f.stage], SEV_ORDER[f.severity]))

    country = form['beneficiaryCountry'].value or form['residenceCountry'].value
    name = form['beneficiaryName'].value or (_f(c_ext, 'sellerName').value if _f(c_ext, 'sellerName') else '')
    kbe = suggest_kbe(country, name) if (country or name) else None
    return PaymentReport(findings=uniq, form=form, extractions=extractions, contract=summary, fees=fees,
                         balance=balance, value_dates=value_dates(currency, today), kbe=kbe)

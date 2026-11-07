"""Предзаполнение 25 полей формы перевода из извлечённых данных, находок и справочников."""
from __future__ import annotations

import re

from . import refdata
from .models import FIELD_IDS, CaseInput, ContractSummary, ExtractionResult, FieldOption, FieldState, Finding
from .rules.balance import account_for_currency
from .rules.common import fmt_date, fmt_money
from .rules.dates import cutoff_for, parse_date, value_dates
from .rules.fees import simulate_fees
from .rules.iban import normalize_iban, validate_iban
from .rules.kbe import suggest_kbe
from .rules.purpose import build_purpose_template
from .rules.swift import normalize_swift

PREPAY_RX = re.compile(r'pre-?payment|advance\s+payment|in\s+advance|предоплат|аванс', re.I)


def _get(ext: ExtractionResult | None, key: str):
    return ext.fields.get(key) if ext else None


def _country_name(code):
    for c in refdata.load('countries'):
        if c['code'] == code:
            return c['name']
    return code


def _fee_from_contract(case: CaseInput) -> tuple[str | None, object]:
    """Тип комиссии из пункта договора о банковских комиссиях."""
    from .rules.bilingual import align_clauses
    for d in case.docs:
        if d.kind != 'contract':
            continue
        for p in align_clauses(d):
            t = p.en.quote if p.en else ''
            if not re.search(r'bank\s+(charges|commissions?|fees)', t, re.I):
                continue
            if re.search(r"Buyer's\s+bank[^;.]*borne\s+by\s+the\s+Buyer[^.]*other[^.]*borne\s+by\s+the\s+Seller", t, re.I):
                return 'SHA', p.en
            if re.search(r'all\s+bank\s+charges[^.]*borne\s+by\s+the\s+Buyer', t, re.I):
                return 'OUR', p.en
            if re.search(r'all\s+bank\s+charges[^.]*borne\s+by\s+the\s+Seller', t, re.I):
                return 'BEN', p.en
    return None, None


def build_form(case: CaseInput, extractions: list[ExtractionResult], findings: list[Finding],
               summary: ContractSummary) -> dict[str, FieldState]:
    c_ext = next((e for e in extractions if e.kind == 'contract'), None)
    invs = [e for e in extractions if e.kind == 'invoice']
    invs.sort(key=lambda e: (_get(e, 'invoiceDate').norm if _get(e, 'invoiceDate') else ''))
    inv = invs[-1] if invs else None
    letters = [e for e in extractions if e.kind == 'letter']
    red = {(f.rule, f.id.split(':', 1)[1] if ':' in f.id else '') for f in findings if f.severity == 'red'}
    prev = case.form or {}
    F: dict[str, FieldState] = {}

    cur = (_get(inv, 'currency').norm if _get(inv, 'currency') else None) or summary.currency or 'USD'
    inv_amount = _get(inv, 'invoiceAmount')
    amt = inv_amount.norm if inv_amount and isinstance(inv_amount.norm, float) else None
    today = case.today or case.value_date

    F['requestNo'] = FieldState(value='будет присвоен после сохранения', status='system', confirmed=True,
                                note='Номер заявки присвоит система после сохранения.')

    # счёт списания
    acc = account_for_currency(cur)
    acc_opts = [FieldOption(value=a['id'], label=f'{a["title"]}, {fmt_money(a["balance"], a["currency"])}',
                            why=('Валюта счёта совпадает с валютой платежа, конвертация не нужна.' if a['currency'] == cur
                                 else f'Придётся купить {cur} за {a["currency"]}, банк возьмёт плату за конвертацию.'))
                for a in refdata.accounts()]
    if acc:
        F['debitAccount'] = FieldState(value=acc['id'], status='ai', options=acc_opts,
                                       note=f'Платёж в {cur}, поэтому выбран счёт «{acc["title"]}». На нём '
                                            f'{fmt_money(acc["balance"], cur)}.')
    else:
        kzt = account_for_currency('KZT')
        F['debitAccount'] = FieldState(value='', status='conflict', options=acc_opts,
                                       note=f'У вас нет счёта в {cur}. Можно платить с тенгового счёта с конвертацией: '
                                            f'банк купит {cur} за тенге по курсу с наценкой '
                                            f'{str(refdata.load("rates").get("conversionSpreadPct", 0)).replace(".", ",")}%'
                                            + (f'. На тенговом счёте {fmt_money(kzt["balance"], "KZT")}.' if kzt else '.'))
    debit = F['debitAccount'].value

    # сумма
    over = next((f for f in findings if f.rule == 'over_remaining'), None)
    if amt is not None and not over:
        F['amount'] = FieldState(value=f'{amt:.2f}', status='doc', source=inv_amount.source,
                                 note=f'Сумма из инвойса: {fmt_money(amt, cur)}.')
    elif amt is not None and over:
        rem = summary.remaining or 0
        F['amount'] = FieldState(value='', status='conflict', source=inv_amount.source, suggested=f'{rem:.2f}',
                                 note=f'Инвойс на {fmt_money(amt, cur)} больше остатка по договору '
                                      f'{fmt_money(rem, summary.currency or cur)}.',
                                 options=[FieldOption(value=f'{rem:.2f}', label=f'Остаток по договору: {fmt_money(rem, cur)}',
                                                      why='Столько ещё можно заплатить по договору без дополнительного соглашения.'),
                                          FieldOption(value=f'{amt:.2f}', label=f'Сумма инвойса: {fmt_money(amt, cur)}',
                                                      why='Можно заплатить только после допсоглашения об увеличении суммы договора.')])
    else:
        F['amount'] = FieldState(value='', status='conflict', note='В документах не нашлась сумма к оплате, введите её.')

    # дата валютирования и срочность
    vds = value_dates(cur, today)
    cutoff = cutoff_for(cur)
    F['valueDate'] = FieldState(value=parse_date(case.value_date) or vds.standard, status='ai',
                                note=f'Обычный перевод в {cur}, поданный до {cutoff}, банк обработает в ближайший рабочий '
                                     f'день ({fmt_date(vds.standard)}). Если нужно сегодня, включите срочный перевод: '
                                     f'он уйдёт {fmt_date(vds.urgent)} при подаче до {cutoff}, стоит '
                                     f'{fmt_money(refdata.tariffs()["urgentFeeKzt"], "₸", 0)}.',
                                options=[FieldOption(value=vds.standard, label=f'{fmt_date(vds.standard)}, обычный',
                                                     why='Ближайшая дата без доплаты.'),
                                         FieldOption(value=vds.urgent, label=f'{fmt_date(vds.urgent)}, срочный',
                                                     why='Нужна отметка «Срочный перевод», доплата 5 000 ₸.')])
    F['urgent'] = FieldState(value='false', status='ai',
                             note='Срочность не нужна, если поставщика устраивает обычная дата. Срочный перевод стоит '
                                  f'{fmt_money(refdata.tariffs()["urgentFeeKzt"], "₸", 0)}.')

    # реквизиты получателя
    src_doc = inv or (letters[-1] if letters else None) or c_ext
    other = [e for e in ([inv] if inv else []) + letters if e]

    def _from_doc(key, field, norm_fn, rule_keys, why_contract, why_doc):
        val = _get(src_doc, key)
        cval = _get(c_ext, key)
        conflict_docs = [e for e in other if any((r, e.doc_id) in red for r in rule_keys)]
        if conflict_docs and cval:
            alt = _get(conflict_docs[0], key)
            opts = [FieldOption(value=norm_fn(cval.value), label=f'Из договора: {cval.value}', why=why_contract)]
            if alt and norm_fn(alt.value) != norm_fn(cval.value):
                kind = 'инвойса' if conflict_docs[0].kind == 'invoice' else 'письма'
                opts.append(FieldOption(value=norm_fn(alt.value), label=f'Из {kind}: {alt.value}', why=why_doc))
            F[field] = FieldState(value='', status='conflict', source=(alt or cval).source, options=opts,
                                  suggested=norm_fn(cval.value),
                                  note='Документы противоречат друг другу. Выберите значение только после звонка '
                                       'поставщику по телефону из договора.')
        elif val:
            F[field] = FieldState(value=norm_fn(val.value), status='doc', source=val.source)
        else:
            F[field] = FieldState(value='', status='conflict', note='Значение не найдено в документах, введите вручную.')

    iban_why_c = 'Счёт из статьи 13 подписанного договора.'
    _from_doc('sellerIban', 'beneficiaryIban', normalize_iban, ('requisites_changed', 'iban_checksum', 'iban_format'),
              iban_why_c, 'Счёт из нового документа. Он не совпадает с договором или не проходит проверку, '
                          'платить на него можно только после подтверждения поставщиком.')
    _from_doc('sellerSwift', 'beneficiarySwift', normalize_swift, ('requisites_changed', 'swift_format', 'swift_iban_country'),
              'SWIFT банка из статьи 13 договора.', 'SWIFT из нового документа, не совпадает с договором.')
    _from_doc('sellerName', 'beneficiaryName', lambda v: re.sub(r'\s+', ' ', v).strip(), ('beneficiary_name_mismatch',),
              'Название продавца из договора.', 'Название из инвойса, оно отличается от договора.')
    if F['beneficiaryIban'].status == 'doc':
        v = validate_iban(F['beneficiaryIban'].value)
        F['beneficiaryIban'].note = ('IBAN проходит проверку контрольной суммы.' if v['ok'] else f'IBAN не проходит проверку: {v["hint"]}.')
    if F['beneficiarySwift'].status == 'doc':
        b = refdata.bank_by_swift(F['beneficiarySwift'].value)
        if b:
            F['beneficiarySwift'].note = f'Банк получателя: {b["name"]}, {b["city"]}.'

    inter = _get(inv, 'intermediaryBank')
    if inter:
        F['intermediaryBank'] = FieldState(value=inter.value, status='doc', source=inter.source,
                                           note='Банк-посредник указан поставщиком в инвойсе.')
    else:
        F['intermediaryBank'] = FieldState(value='', status='empty',
                                           note='Поставщик не указал банк-посредник. Обычно поле можно оставить пустым, '
                                                'банк выберет маршрут сам.')

    # КНП
    task = case.task or {}
    terms = _get(inv, 'paymentTerms')
    prepay = task.get('timing') == 'prepay' or (task.get('timing') != 'postpay' and bool(terms and PREPAY_RX.search(terms.value)))
    services = task.get('goods') == 'services'
    knp = refdata.load('knp')
    code = '780' if services else ('711' if prepay else '710')
    why_map = {'710': 'Подходит, если товар уже отгружен или оплата после поставки.',
               '711': 'Подходит для предоплаты: инвойс выставлен до отгрузки.',
               '780': 'Для оплаты услуг, а не товара.', '859': 'Только если платёж не подходит ни под один код.'}
    knp_opts = [FieldOption(value=i['code'], label=f'{i["code"]} {i["title"]}', why=why_map.get(i['code'], i['when']))
                for i in knp['items']]
    reason = ('Задача на стартовом экране: услуги.' if services else
              ('В инвойсе условие оплаты «' + terms.value + '», это предоплата.' if prepay and terms else
               'Вы указали, что платите до поставки.' if prepay else 'Оплата за товар по контракту.'))
    F['knp'] = FieldState(value=code, status='ai', options=knp_opts,
                          note=f'{reason} Код {code}. Сверьте его с классификатором Национального Банка РК '
                               '(проверка у ментора).')

    # назначение
    inv_no, inv_date = _get(inv, 'invoiceNo'), _get(inv, 'invoiceDate')
    c_no, c_date = _get(c_ext, 'contractNo'), _get(c_ext, 'contractDate')
    goods = None
    for d in case.docs:
        if d.kind == 'contract':
            m = re.search(r'\b((?:[a-z]+ )?equipment)\b', d.text, re.I)
            if m:
                words = m.group(1).split()
                if words[0].lower() in ('the', 'of', 'and', 'for', 'a', 'an', 'all', 'any', 'with', 'buys', 'sells'):
                    words = words[1:]
                goods = ' '.join(words)
            break
    if not goods and _get(inv, 'goodsDescription'):
        goods = _get(inv, 'goodsDescription').value
    purpose = build_purpose_template(inv_no.value if inv_no else None, inv_date.norm if inv_date else None,
                                     c_no.value if c_no else None, c_date.norm if c_date else None, goods,
                                     case.accounting_number, prepay)
    F['purpose'] = FieldState(value=purpose, status='ai',
                              note='Собрано из номера и даты инвойса и договора латиницей, не длиннее 140 символов'
                                   + (', с учётным номером договора.' if case.accounting_number and 'UN ' in purpose else '.'))

    # привязка договора
    if case.accounting_number:
        F['linkContract'] = FieldState(value=case.accounting_number, status='system',
                                       note='Учётный номер договора, под ним договор стоит на учёте в банке.')
    elif summary.needs_accounting_number:
        F['linkContract'] = FieldState(value='', status='conflict',
                                       note='Сумма договора больше порога, нужен учётный номер. Его пока нет: поставьте '
                                            'договор на учёт и впишите номер.',
                                       options=[FieldOption(value=c_no.value, label=f'Договор {c_no.value}',
                                                            why='Подойдёт только после получения учётного номера.')] if c_no else None)
    elif c_no:
        F['linkContract'] = FieldState(value=c_no.value, status='ai', source=c_no.source,
                                       note='Учётный номер не нужен, привязываем договор по номеру.')
    else:
        F['linkContract'] = FieldState(value='', status='empty', note='Договор не загружен.')

    F['docNumber'] = (FieldState(value=inv_no.value, status='doc', source=inv_no.source) if inv_no
                      else FieldState(value=c_no.value, status='doc', source=c_no.source) if c_no
                      else FieldState(value='', status='empty'))
    F['docDate'] = (FieldState(value=inv_date.norm, status='doc', source=inv_date.source) if inv_date and inv_date.norm
                    else FieldState(value=c_date.norm, status='doc', source=c_date.source) if c_date and c_date.norm
                    else FieldState(value='', status='empty'))
    names = [d.name for d in case.docs]
    F['attachments'] = FieldState(value='; '.join(names), status='doc' if names else 'empty',
                                  note='Приложим все загруженные документы.' if names else None)

    # страна
    ctry = _get(inv, 'sellerCountry') or _get(c_ext, 'sellerCountry')
    if ctry and ctry.norm:
        nm = _country_name(ctry.norm)
        F['residenceCountry'] = FieldState(value=ctry.norm, status='doc', source=ctry.source,
                                           note=f'По адресу продавца: {nm}.')
        F['beneficiaryCountry'] = FieldState(value=ctry.norm, status='doc', source=ctry.source,
                                             note=f'Страна из адреса регистрации получателя: {nm}.')
    else:
        F['residenceCountry'] = FieldState(value='', status='conflict', note='Страна получателя не найдена.')
        F['beneficiaryCountry'] = FieldState(value='', status='conflict', note='Страна получателя не найдена.')
    name_for_kbe = F['beneficiaryName'].value or (_get(c_ext, 'sellerName').value if _get(c_ext, 'sellerName') else '')
    kbe = suggest_kbe(F['residenceCountry'].value, name_for_kbe)
    F['kbe'] = FieldState(value=kbe.code, status='ai', note=kbe.why + ' Нужно сверить со справочником.')
    F['addressType'] = FieldState(value='legal', status='ai', note='В реквизитах указан адрес регистрации компании.')

    # комиссия
    pay_amt = amt or 0.0
    sims = {t: simulate_fees(pay_amt, cur, t) for t in ('OUR', 'SHA', 'BEN')}
    fee_opts = [
        FieldOption(value='OUR', label='OUR: все комиссии платите вы',
                    why=f'Спишут {fmt_money(sims["OUR"].sender_total, cur)}, поставщик получит всю сумму '
                        f'{fmt_money(sims["OUR"].receiver_gets, cur)}.'),
        FieldOption(value='SHA', label='SHA: комиссии пополам',
                    why=f'Спишут {fmt_money(sims["SHA"].sender_total, cur)}, поставщик получит около '
                        f'{fmt_money(sims["SHA"].receiver_gets, cur)}, недополучит {fmt_money(sims["SHA"].shortfall, cur)}.'),
        FieldOption(value='BEN', label='BEN: все комиссии за счёт получателя',
                    why=f'Спишут {fmt_money(sims["BEN"].sender_total, cur)}, поставщик получит около '
                        f'{fmt_money(sims["BEN"].receiver_gets, cur)}, недополучит {fmt_money(sims["BEN"].shortfall, cur)}.'),
    ]
    ft, ft_src = _fee_from_contract(case)
    if ft:
        F['feeType'] = FieldState(value=ft, status='doc', source=ft_src, options=fee_opts,
                                  note=f'В договоре сказано, кто платит комиссии, это соответствует типу {ft}.')
    else:
        F['feeType'] = FieldState(value='OUR', status='ai', options=fee_opts,
                                  note='В договоре не сказано, кто платит комиссии. С SHA или BEN поставщик получит '
                                       'меньше суммы инвойса, и формально это недоплата. OUR надёжнее: '
                                       f'{fee_opts[0].why}')
    F['feeAccount'] = FieldState(value=debit, status='ai' if debit else 'conflict',
                                 note='Комиссию спишем с того же счёта, что и перевод.' if debit
                                 else 'Сначала выберите счёт списания.')
    cl = refdata.client()
    F['signerHead'] = FieldState(value=cl['director'], status='ai', note='Руководитель компании из карточки клиента.')
    F['signerAccountant'] = FieldState(value='Не предусмотрен', status='system', confirmed=True,
                                       note='Вторая подпись не настроена.')
    F['useBonuses'] = FieldState(value='false', status='ai',
                                 note=f'Бонусами можно покрыть не больше {refdata.tariffs()["bonusMaxSharePct"]}% комиссии. '
                                      'Включите, если хотите сэкономить.')
    F['acceptTerms'] = FieldState(value='false', status='empty', note='Отметку ставит только человек.')

    # suggested, пользовательские значения, подтверждения
    out: dict[str, FieldState] = {}
    for fid in FIELD_IDS:
        st = F.get(fid) or FieldState()
        if st.suggested is None:
            st.suggested = st.value
        p = prev.get(fid)
        if p is not None and fid != 'acceptTerms':
            if p.status == 'user':
                out[fid] = p.model_copy(update={'suggested': st.suggested})
                continue
            if p.confirmed and p.value == st.value:
                st.confirmed = True
        if fid == 'acceptTerms':
            if p is not None and p.status == 'user':
                out[fid] = p
                continue
            st = FieldState(value='false', status='empty', note=st.note, suggested='false')
        out[fid] = st
    return out

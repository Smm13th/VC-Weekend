"""Извлечение реквизитов по меткам из samples_spec.md. Каждое значение несёт точную цитату (SourceRef)."""
from __future__ import annotations

import re


from .models import DocBlock, ExtractionResult, Extracted, ParsedDoc
from .rules.common import has_placeholder, ref
from .rules.dates import parse_date
from .rules.iban import normalize_iban
from .rules.money import parse_amount
from .rules.swift import normalize_swift

DATE = r'\d{1,2}\.\d{1,2}\.\d{4}'
NUM = r'\d{1,3}(?:[   ,.]\d{3})*(?:[.,]\d{1,2})?|\d+(?:[.,]\d{1,2})?'
CUR = r'USD|EUR|KZT|CNY|RUB|AED|GBP|CHF|TRY|GEL|JPY|UZS'
MONEY = rf'(?:(?P<cur>{CUR})\s?(?P<num>{NUM})|(?P<num2>{NUM})\s?(?P<cur2>{CUR}))'

EXTRA_KEYS = ['intermediaryBank']

# Соответствие названий стран кодам (для адресов)
_COUNTRY_ALIASES = {
    'AE': ['United Arab Emirates', 'UAE', 'U.A.E.', 'ОАЭ', 'Объединённые Арабские Эмираты', 'Объединенные Арабские Эмираты'],
    'GE': ['Georgia', 'Грузия'], 'KZ': ['Republic of Kazakhstan', 'Kazakhstan', 'Казахстан'],
    'TR': ['Turkey', 'Türkiye', 'Турция'], 'DE': ['Germany', 'Германия'], 'GB': ['United Kingdom', 'UK', 'Великобритания'],
    'CN': ["People's Republic of China", 'China', 'Китай', 'КНР'], 'US': ['United States', 'USA', 'США'],
    'RU': ['Russian Federation', 'Russia', 'Россия'], 'UZ': ['Uzbekistan', 'Узбекистан'],
}


def _country_regex():
    alts = []
    for code, names in _COUNTRY_ALIASES.items():
        for n in names:
            alts.append((n, code))
    alts.sort(key=lambda x: -len(x[0]))
    return alts


_COUNTRIES = _country_regex()


def find_country(text: str):
    """-> (code, start, end) последнего упоминания страны в адресе"""
    best = None
    for name, code in _COUNTRIES:
        pat = r'(?<![A-Za-zА-Яа-я])' + r'\s+'.join(re.escape(p) for p in name.split()) + r'(?![A-Za-zА-Яа-я])'
        for m in re.finditer(pat, text):
            if best is None or m.start() > best[1] or (m.start() == best[1] and m.end() > best[2]):
                best = (code, m.start(), m.end())
    return best


class _Ctx:
    def __init__(self, doc: ParsedDoc):
        self.doc = doc
        self.fields: dict[str, Extracted] = {}

    def scan(self, pattern: str, blocks, flags=re.M | re.I, group='v'):
        out = []
        rx = re.compile(pattern, flags)
        for b in blocks:
            t = self.doc.text[b.start:b.end]
            for m in rx.finditer(t):
                if m.group(group) is None:
                    continue
                s, e = m.span(group)
                raw = m.group(group)
                # обрезаем пробелы и финальную точку/запятую
                while e > s and (self.doc.text[b.start + e - 1] in ' \t ;'):
                    e -= 1
                while s < e and self.doc.text[b.start + s] in ' \t ':
                    s += 1
                if s >= e:
                    continue
                out.append((b.start + s, b.start + e, m, b))
                del raw
        return out

    def put(self, key, hits, norm_fn=lambda v, m: v, conf='high', label=None):
        """hits: список (start, end, match, block). Берёт первое значение без плейсхолдера; разные значения -> conflict."""
        if key in self.fields:
            return True
        good = []
        for s, e, m, b in hits:
            val = self.doc.text[s:e]
            if has_placeholder(val) or not val.strip() or re.fullmatch(r'[_\s.\-]+', val):
                continue
            try:
                nv = norm_fn(val, m)
            except Exception:
                nv = None
            good.append((s, e, val, nv))
        if not good:
            return False
        s, e, val, nv = good[0]
        src = ref(self.doc, s, e)
        distinct = []
        for g in good:
            k = g[3] if g[3] is not None else g[2]
            if isinstance(k, str):
                k = re.sub(r'\s+', ' ', k).strip().lower()
            if k not in distinct:
                distinct.append(k)
        ex = Extracted(key=key, value=re.sub(r'\s*\n\s*', ' ', src.quote), norm=nv, source=src, confidence=conf)
        if len(distinct) > 1:
            ex.status = 'conflict'
            vals = '; '.join(dict.fromkeys(re.sub(r'\s+', ' ', g[2]) for g in good))
            ex.note = f'В документе {"метка «" + label + "»" if label else "это поле"} встречается с разными значениями: {vals}'
            ex.confidence = 'low'
        self.fields[key] = ex
        return True


def _n_text(v, m):
    return re.sub(r'\s+', ' ', v).strip()


def _n_date(v, m):
    return parse_date(v)


def _n_amount(v, m):
    return parse_amount(v)


def _n_iban(v, m):
    return normalize_iban(v)


def _n_swift(v, m):
    return normalize_swift(v)


def _label(lbl: str, value: str = r'[^\n]*') -> str:
    return rf'^[ \t]*(?:{lbl})[ \t]*:[ \t]*(?P<v>{value})$'


def _first_line_starts(doc, b, pattern) -> bool:
    first = doc.text[b.start:b.end].split('\n', 1)[0]
    return re.match(pattern, first.strip(), re.I) is not None


def _money_cur(ctx: _Ctx, key_amount: str, hits, conf='high'):
    """amount + currency из одной цитаты"""
    if not ctx.put(key_amount, hits, _n_amount, conf):
        return
    # currency из той же цитаты, что и сумма
    src = ctx.fields[key_amount].source
    for s, e, m, b in hits:
        if s == src.start:
            g = 'cur' if m.group('cur') else 'cur2'
            cs, ce = m.span(g)
            cs += b.start
            ce += b.start
            if 'currency' not in ctx.fields:
                ctx.fields['currency'] = Extracted(key='currency', value=ctx.doc.text[cs:ce],
                                                   norm=ctx.doc.text[cs:ce].upper(), source=ref(ctx.doc, cs, ce),
                                                   confidence=conf)
            break


def _country_from(ctx: _Ctx, key: str, addr_key: str, iban_key: str | None):
    if key in ctx.fields:
        return
    a = ctx.fields.get(addr_key)
    if a:
        seg = ctx.doc.text[a.source.start:a.source.end]
        c = find_country(seg)
        if c:
            code, s, e = c
            src = ref(ctx.doc, a.source.start + s, a.source.start + e)
            ctx.fields[key] = Extracted(key=key, value=re.sub(r'\s+', ' ', src.quote), norm=code, source=src,
                                        confidence='high')
            return
    if iban_key and iban_key in ctx.fields:
        ib = ctx.fields[iban_key]
        if re.match(r'[A-Z]{2}', ib.source.quote):
            src = ref(ctx.doc, ib.source.start, ib.source.start + 2)
            ctx.fields[key] = Extracted(key=key, value=src.quote, norm=src.quote, source=src, confidence='medium',
                                        note='Страна определена по первым буквам IBAN')


def _intermediary_norm(v, m):
    mm = re.search(r'\b[A-Z]{4}[A-Z]{2}[A-Z0-9]{2}(?:[A-Z0-9]{3})?\b', v)
    return mm.group(0) if mm else re.sub(r'\s+', ' ', v).strip()


def _extract_contract(ctx: _Ctx):
    doc = ctx.doc
    has_lang = any(b.lang for b in doc.blocks)
    en = [b for b in doc.blocks if b.lang == 'en'] if has_lang else list(doc.blocks)
    ru = [b for b in doc.blocks if b.lang == 'ru']
    head_en = en[:3]
    head_ru = ru[:3]

    ctx.put('contractNo', ctx.scan(_label_free(r'Ref\.?(?:\s*No\.?)?', r'\S[^\n]*?'), head_en), _n_text) or \
        ctx.put('contractNo', ctx.scan(r'(?:Contract|CONTRACT)\s+No\.?\s*:?\s*(?P<v>[A-Z0-9][\w/\-.]*)', en), _n_text) or \
        ctx.put('contractNo', ctx.scan(_label_free(r'Ref\.?|№', r'\S[^\n]*?'), head_ru), _n_text, 'medium')
    ctx.put('contractDate', ctx.scan(_label(r'Date', DATE), head_en), _n_date) or \
        ctx.put('contractDate', ctx.scan(_label(r'Дата', DATE), head_ru), _n_date, 'medium')
    ctx.put('expiryDate', ctx.scan(rf'valid\s+(?:until|till|through)\s+(?P<v>{DATE})', en), _n_date) or \
        ctx.put('expiryDate', ctx.scan(rf'действ\w*\s+до\s+(?P<v>{DATE})', ru), _n_date, 'medium')
    hits = ctx.scan(rf'total\s+(?:value|amount|price)\s+of\s+(?:this|the)\s+Contract\s+(?:is|amounts\s+to)\s+(?P<v>{MONEY})', en)
    if hits:
        _money_cur(ctx, 'totalAmount', hits)
    else:
        hits = ctx.scan(rf'стоимость[^\n]{{0,60}}?(?:составляет|равна)\s+(?P<v>{MONEY})', ru)
        if hits:
            _money_cur(ctx, 'totalAmount', hits, 'medium')
    if 'currency' not in ctx.fields:
        ctx.put('currency', ctx.scan(rf'currency\s+of\s+the\s+Contract\s+is[^\n(]*\((?P<v>{CUR})\)', en),
                lambda v, m: v.upper())

    seller_en = [b for b in en if _first_line_starts(doc, b, r'Seller\s*:')] or en
    seller_ru = [b for b in ru if _first_line_starts(doc, b, r'Продавец\s*:')]
    buyer_en = [b for b in en if _first_line_starts(doc, b, r'Buyer\s*:')] or en
    buyer_ru = [b for b in ru if _first_line_starts(doc, b, r'Покупатель\s*:')]

    def lab(key, en_lbl, ru_lbl=None, norm=_n_text, blocks_en=seller_en, blocks_ru=seller_ru, label=None):
        if ctx.put(key, ctx.scan(_label(en_lbl), blocks_en), norm, 'high', label):
            return True
        if ru_lbl and blocks_ru:
            return ctx.put(key, ctx.scan(_label(ru_lbl), blocks_ru), norm, 'medium', label)
        return False

    lab('sellerName', r'Seller', r'Продавец', label='Seller')
    if 'sellerName' not in ctx.fields:
        ctx.put('sellerName', ctx.scan(r'^(?P<v>[^\n(]+?)\s*\(hereinafter\s+(?:referred\s+to\s+as\s+)?(?:the\s+)?.?Seller', en),
                _n_text, 'medium')
    ctx.put('sellerRepresentative',
            ctx.scan(r'Seller.{0,3}\)\s*,?\s*represented\s+by\s+(?P<v>[^,\n]+)', en), _n_text) or \
        ctx.put('sellerRepresentative', ctx.scan(r'Продав\w+.{0,3}\s*,?\s*в\s+лице\s+(?P<v>[^,\n]+)', ru), _n_text, 'medium')
    lab('sellerAddress', r'Office\s+Address|Legal\s+Address|Address', r'Адрес\s+офиса|Юридический\s+адрес')
    lab('sellerAddress', r'License\s+Address', r'Адрес\s+по\s+лицензии')
    lab('sellerLicenseNo', r'Trade\s+License(?:\s+Registration)?\s+No\.?|License\s+No\.?', r'Номер\s+торговой\s+лицензии')
    lab('sellerBank', r'Beneficiary\s+Bank|Bank\s+Name|Bank', r'Банк\s+получателя|Банк')
    lab('sellerBankAddress', r'Bank\s+Address', r'Адрес\s+банка')
    lab('sellerAccountName', r'Account\s+Name', r'Наименование\s+счёта|Наименование\s+счета')
    lab('sellerAccountNo', r'Account\s+No\.?|Account\s+Number', r'Номер\s+сч[её]та')
    lab('sellerIban', r'IBAN', r'IBAN', _n_iban, label='IBAN')
    lab('sellerSwift', r'Swift\s+Code|SWIFT|SWIFT/BIC|BIC', r'SWIFT|БИК', _n_swift, label='Swift Code')
    _country_from(ctx, 'sellerCountry', 'sellerAddress', 'sellerIban')

    lab('buyerName', r'Buyer', r'Покупатель', blocks_en=buyer_en, blocks_ru=buyer_ru)
    lab('buyerAddress', r'Legal\s+address', r'Юридический\s+адрес', blocks_en=buyer_en, blocks_ru=buyer_ru)
    cur = ctx.fields.get('currency')
    curv = cur.norm if cur else 'USD'
    if not lab('buyerIban', rf'Settlement\s+account\s+{curv}|IBAN', rf'Расч[её]тный\s+сч[её]т\s+{curv}', _n_iban,
               blocks_en=buyer_en, blocks_ru=buyer_ru):
        lab('buyerIban', r'Settlement\s+account(?:\s+[A-Z]{3})?', r'Расч[её]тный\s+сч[её]т(?:\s+[A-Z]{3})?', _n_iban,
            blocks_en=buyer_en, blocks_ru=buyer_ru)
    lab('buyerBic', r'BIC|SWIFT', r'БИК', _n_swift, blocks_en=buyer_en, blocks_ru=buyer_ru)
    pay = ctx.scan(r'^\s*4\.\d+\.?\s+(?P<v>[^\n]*\bpa(?:y|id|yment)\b[^\n]*\b(?:within|not\s+later\s+than)\s+\d+[^\n]*)$', en)
    ctx.put('paymentTerms', pay, _n_text, 'medium')


def _label_free(lbl, value):
    return rf'^[ \t]*(?:{lbl})[ \t]*:?[ \t]*(?P<v>{value})[ \t]*$'


def _extract_invoice_like(ctx: _Ctx, kind: str):
    doc = ctx.doc
    bl = list(doc.blocks)
    # для меток, которые в PDF могут перенестись на следующую строку, ищем по всему тексту
    whole = [DocBlock(start=0, end=len(doc.text))]
    if kind != 'letter':
        ctx.put('invoiceNo', ctx.scan(r'^\s*(?:Commercial\s+)?Invoice\s*(?:No\.?|Number|№)\s*:?\s*(?P<v>[A-Z0-9][\w/\-.]*)', bl),
                _n_text, label='Invoice No')
        ctx.put('invoiceDate', ctx.scan(_label(r'(?:Invoice\s+)?Date|Дата', DATE), bl), _n_date, label='Date')
        hits = ctx.scan(rf'^\s*(?:Total(?:\s+amount)?(?:\s+due)?|Grand\s+total|Итого)\s*:?\s*(?P<v>{MONEY})', bl)
        if hits:
            _money_cur(ctx, 'invoiceAmount', hits)
            # разные суммы «Total» в одном документе
        ctx.put('invoiceDueDate', ctx.scan(rf'due\s+date\s*:?\s*(?P<v>{DATE})', whole), _n_date)
        ctx.put('paymentTerms', ctx.scan(_label(r'Payment\s+terms'), bl), _n_text)
        ctx.put('deliveryTerms', ctx.scan(_label(r'Delivery\s+terms|Terms\s+of\s+delivery'), bl), _n_text)
        ctx.put('goodsDescription',
                ctx.scan(rf'^\s*1\.?\s+(?P<v>\S.*?)\s+\d+(?:[.,]\d+)?\s+(?:{NUM})\s+(?:{NUM})\s*$', bl), _n_text, 'medium')
    ref_pat = rf'(?P<v>(?P<no>[A-Z0-9][\w/\-.]*?)\s+(?:dated|dd\.?|of|from|от)\s+(?P<d>{DATE}))'
    ref_norm = lambda v, m: f'{m.group("no")}|{parse_date(m.group("d"))}'  # noqa: E731
    if kind == 'letter':
        ctx.put('invoiceContractRef', ctx.scan(rf'Ref\.?\s*:?\s*Contract\s+(?:No\.?\s*)?{ref_pat}', whole), ref_norm) or \
            ctx.put('invoiceContractRef', ctx.scan(rf'Contract\s+(?:No\.?\s*)?{ref_pat}', whole), ref_norm, 'medium')
    else:
        ctx.put('invoiceContractRef', ctx.scan(rf'Contract\s*(?:Ref\.?|No\.?|Reference)\s*:?\s*{ref_pat}', whole), ref_norm) or \
            ctx.put('invoiceContractRef', ctx.scan(r'Contract\s*(?:Ref\.?|No\.?|Reference)\s*:?\s*(?P<v>[A-Z0-9][\w/\-.]*)', bl),
                    lambda v, m: f'{v}|', 'medium')
    ctx.put('sellerName', ctx.scan(_label(r'Beneficiary(?:\s+Name)?|Account\s+Name'), bl), _n_text, label='Beneficiary')
    seller_hits = ctx.scan(r'^\s*Seller\s*:\s*(?P<v>[^,\n]+)', bl)
    if 'sellerName' not in ctx.fields:
        ctx.put('sellerName', seller_hits, _n_text, 'medium')
    # адрес продавца: после первой запятой в строке Seller, с продолжением при переносе строки в PDF
    for s, e, m, b in seller_hits[:1]:
        line_end = doc.text.find('\n', e)
        line_end = len(doc.text) if line_end < 0 else line_end
        a_start = e + 1 if e < len(doc.text) and doc.text[e] == ',' else e
        a_end = line_end
        nxt = line_end + 1
        while nxt < len(doc.text):
            ne = doc.text.find('\n', nxt)
            ne = len(doc.text) if ne < 0 else ne
            line = doc.text[nxt:ne]
            if ':' in line or not line.strip() or re.match(r'^\s*(No\b|\d+\s)', line):
                break
            a_end = ne
            nxt = ne + 1
        if a_end > a_start:
            src = ref(doc, a_start, a_end)
            if src.quote:
                ctx.fields['sellerAddress'] = Extracted(key='sellerAddress', value=re.sub(r'\s*\n\s*', ' ', src.quote),
                                                        norm=None, source=src, confidence='medium')
    ctx.put('buyerName', ctx.scan(r'^\s*Buyer\s*:\s*(?P<v>[^,\n]+)', bl), _n_text)
    ctx.put('sellerBank', ctx.scan(_label(r'Beneficiary(?:\'s)?\s+Bank|Bank\s+of\s+Beneficiary|Bank\s+Name'), bl), _n_text,
            label='Beneficiary Bank')
    ctx.put('sellerAccountNo', ctx.scan(_label(r'Account\s+No\.?|Account\s+Number'), bl), _n_text)
    ctx.put('sellerIban', ctx.scan(_label(r'IBAN', r'[A-Za-z]{2}[\dA-Za-z  ]{8,40}?'), bl), _n_iban, label='IBAN')
    ctx.put('sellerSwift', ctx.scan(_label(r'Swift(?:\s+Code)?|SWIFT/BIC|BIC', r'[A-Za-z0-9 ]{8,14}?'), bl), _n_swift,
            label='Swift Code')
    ctx.put('intermediaryBank', ctx.scan(_label(r'Intermediary(?:\s+Bank)?|Correspondent\s+Bank'), bl), _intermediary_norm)
    _country_from(ctx, 'sellerCountry', 'sellerAddress', 'sellerIban')


def extract_fields(doc: ParsedDoc) -> ExtractionResult:
    ctx = _Ctx(doc)
    if doc.kind == 'contract':
        _extract_contract(ctx)
    else:
        _extract_invoice_like(ctx, doc.kind)
    order = {k: i for i, k in enumerate(list(refdata_keys()))}
    fields = dict(sorted(ctx.fields.items(), key=lambda kv: order.get(kv[0], 999)))
    return ExtractionResult(doc_id=doc.id, kind=doc.kind, fields=fields)


def refdata_keys():
    from .models import EXTRACT_KEYS
    return EXTRACT_KEYS + EXTRA_KEYS




"""IBAN, SWIFT, суммы, назначение, комиссии, остаток, КБе, даты валютирования."""
import pytest

import docgen as g
from app.rules import (build_purpose_template, check_balance, contract_balance, convert, format_iban, mask_iban,
                       parse_amount, simulate_fees, suggest_kbe, validate_iban, validate_purpose, validate_swift,
                       value_dates)
from app.models import Payment


@pytest.mark.parametrize('iban', [g.SELLER_IBAN, g.SELLER_IBAN_2, g.FRAUD_IBAN, 'KZ49 9980 0100 0041 2577',
                                  'KZ23 9980 0100 0041 2560', 'KZ59 9980 0100 0041 2591', 'KZ96 9980 0100 0041 2604'])
def test_valid_ibans(iban):
    r = validate_iban(iban)
    assert r['ok'] and r['error'] is None
    assert r['normalized'] == iban.replace(' ', '')


def test_typo_iban_with_reference_hint():
    r = validate_iban(g.TYPO_IBAN, reference=g.SELLER_IBAN)
    assert not r['ok'] and r['error'] == 'checksum'
    assert '22-23' in r['hint'] and '"54"' in r['hint'] and '"45"' in r['hint'] and 'перестановку' in r['hint']
    assert r['candidates'] == ['AE980710123456789012345']


def test_typo_iban_without_reference_candidates():
    r = validate_iban(g.TYPO_IBAN)
    assert r['error'] == 'checksum'
    assert 1 <= len(r['candidates']) <= 3
    assert 'AE980710123456789012345' in r['candidates']
    assert all(validate_iban(c)['ok'] for c in r['candidates'])


def test_iban_length_and_format():
    r = validate_iban('AE98 0710 1234 5678 9012 34')
    assert r['error'] == 'length' and '23' in r['hint']
    assert validate_iban('98AE07101234')['error'] == 'format'
    assert validate_iban('AE98 0710 1234 5678 9012 3!5')['error'] == 'format'
    assert validate_iban('US12 3456 7890 1234 5678')['error'] == 'format'   # в США IBAN нет


def test_iban_format_mask():
    assert format_iban('AE980710123456789012345') == g.SELLER_IBAN
    assert mask_iban(g.SELLER_IBAN) == 'AE98 **** 2345'


def test_swift():
    r = validate_swift('DPBLAEAD')
    assert r['ok'] and r['country'] == 'AE' and r['bank']['name'] == 'Desert Pearl Bank PJSC'
    assert validate_swift('dpblaeadxxx')['ok']
    assert validate_swift('DPBLAEADXXX')['bank'] is not None
    r = validate_swift('KVRTGE2')
    assert not r['ok'] and r['error'] == 'format'
    assert not validate_swift('1PBLAEAD')['ok']
    assert validate_swift('ABCDKZKX')['bank'] is None


@pytest.mark.parametrize('s,v', [('51,000.00', 51000.0), ('51 000,00', 51000.0), ('USD 51,000.00', 51000.0),
                                 ('51 000,00', 51000.0), ('51.000,00', 51000.0), ('48000', 48000.0),
                                 ('EUR 48,000.00', 48000.0), ('1,5', 1.5), ('38,500.00 USD', 38500.0),
                                 ('1 234 567,89', 1234567.89), ('', None), ('нет суммы', None), (25000, 25000.0)])
def test_parse_amount(s, v):
    assert parse_amount(s) == v


def test_convert():
    assert convert(100, 'USD', 'USD') == 100
    assert convert(1, 'USD', 'KZT') == 512.40
    assert abs(convert(48000, 'EUR', 'USD') - 52093.68) < 0.01


def test_purpose_template_and_validation():
    s = build_purpose_template('GHM-INV-0412', '2026-10-02', 'AI-DXB-2026/014', '15.09.2026', 'industrial equipment',
                               prepay=True)
    assert s == 'PREPAYMENT FOR INDUSTRIAL EQUIPMENT INV GHM-INV-0412 DD 02.10.2026 CONTRACT AI-DXB-2026/014 DD 15.09.2026'
    assert validate_purpose(s)['ok']
    s2 = build_purpose_template('GHM-INV-0412', '2026-10-02', 'AI-DXB-2026/014', '2026-09-15', 'Оборудование',
                                accounting_number='02/2026/00731')
    assert s2.startswith('PAYMENT FOR OBORUDOVANIE') and s2.endswith('UN 02/2026/00731')
    assert validate_purpose(s2)['ok'] and len(s2) <= 140
    long = build_purpose_template('X' * 30, '2026-10-02', 'Y' * 30, '2026-09-15', 'very long goods name ' * 6,
                                  accounting_number='02/2026/00731')
    assert len(long) <= 140 and validate_purpose(long)['ok']


def test_purpose_issues():
    t = 'Оплата по инвойсу №5'
    r = validate_purpose(t)
    assert not r['ok']
    lat = [i for i in r['issues'] if i['rule'] == 'purpose_latin'][0]
    assert lat['positions'][0] == 0 and t.index('№') in lat['positions'] and 6 not in lat['positions']
    r = validate_purpose('A' * 141)
    assert [i['rule'] for i in r['issues']] == ['purpose_length'] and r['length'] == 141
    assert validate_purpose("PAYMENT (INV 1/2) - OK? +:.,'")['ok']


def test_fees_three_types_sum_up():
    our, sha, ben = (simulate_fees(51000, 'USD', t) for t in ('OUR', 'SHA', 'BEN'))
    assert our.sender_fees == 102 + 30 and our.receiver_gets == 51000 and our.shortfall == 0
    assert sha.sender_fees == 102 and sha.receiver_gets == 51000 - 25 - 15
    assert ben.sender_fees == 0 and ben.receiver_gets == 51000 - 102 - 25 - 15
    for s in (our, sha, ben):
        assert s.sender_total == round(51000 + s.sender_fees, 2)
        assert round(s.receiver_gets + s.shortfall, 2) == 51000
        assert round(sum(l.amount for l in s.lines if l.who == 'sender'), 2) == s.sender_fees
        assert round(sum(l.amount for l in s.lines if l.who == 'beneficiary'), 2) == s.shortfall
        assert s.urgent_fee_kzt == 0
    assert simulate_fees(1000, 'USD', 'OUR').lines[0].amount == 15       # минимум
    assert simulate_fees(500000, 'USD', 'SHA').sender_fees == 150        # максимум
    assert simulate_fees(500000, 'EUR', 'SHA').sender_fees == 140
    assert simulate_fees(100, 'USD', 'OUR', urgent=True).urgent_fee_kzt == 5000


def test_balance():
    ok = check_balance('acc-usd', 51000, 132, 'USD')
    assert ok.ok and ok.available == 64300 and ok.needed == 51132
    bad = check_balance('acc-usd', 70000, 132, 'USD')
    assert not bad.ok and bad.shortfall == 70132 - 64300
    kinds = {o.kind for o in bad.options}
    assert kinds == {'convert', 'partial'}
    conv = next(o for o in bad.options if o.kind == 'convert')
    assert conv.from_account_id == 'acc-kzt' and abs(conv.amount - 5832 * 512.40 * 1.006) < 0.01
    none = check_balance(None, 48000, 126, 'EUR')
    assert not none.ok and none.available == 0 and none.options[0].kind == 'convert'
    kzt = check_balance('acc-kzt', 48000, 126, 'EUR')
    assert kzt.ok and kzt.currency == 'KZT'


def test_contract_balance():
    assert contract_balance(51000, [Payment(date='2026-10-05', amount=30000, currency='USD', ref='x')]) == (30000, 21000)
    assert contract_balance(51000, []) == (0, 51000)
    assert contract_balance(None, [{'amount': 5, 'currency': 'USD'}]) == (5, None)


def test_kbe():
    k = suggest_kbe('AE', 'Gulf Horizon Machinery FZE')
    assert k.code == '27' and k.mentor_check and 'Национального Банка' in k.why
    assert suggest_kbe('KZ', 'ТОО «Алатау Индастриал»').code == '17'
    assert suggest_kbe('GE', 'Kavkaz Trade Bank JSC').code == '24'
    assert suggest_kbe('AE', 'Omar Al-Rashidi').code == '29'


@pytest.mark.parametrize('today,now,std,urg', [
    ('2026-11-09', None, '2026-11-10', '2026-11-09'),      # понедельник до cutoff
    ('2026-11-09', '16:30', '2026-11-11', '2026-11-10'),   # после cutoff: через один
    ('2026-11-13', None, '2026-11-16', '2026-11-13'),      # пятница -> понедельник
    ('2026-11-07', None, '2026-11-10', '2026-11-09'),      # суббота
])
def test_value_dates(today, now, std, urg):
    v = value_dates('USD', today, now)
    assert (v.standard, v.urgent, v.cutoff) == (std, urg, '15:00')


def test_value_dates_eur_cutoff():
    assert value_dates('EUR', '2026-11-09', '14:30').urgent == '2026-11-10'
    assert value_dates('EUR', '2026-11-09', '13:59').urgent == '2026-11-09'

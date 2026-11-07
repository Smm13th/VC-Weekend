"""Предзаполнение формы: статусы, конфликты при подмене, acceptTerms, пользовательские поля."""
from conftest import make_case
from app.extract import extract_fields
from app.models import FIELD_IDS, FieldState
from app.prefill import build_form
from app.rules import check_payment


def test_all_fields_kit_a():
    rep = check_payment(make_case('A'))
    f = rep.form
    assert list(f) == FIELD_IDS
    assert f['requestNo'].status == 'system'
    assert f['debitAccount'].value == 'acc-usd' and f['debitAccount'].status == 'ai' and '64 300' in f['debitAccount'].note
    assert f['amount'].value == '51000.00' and f['amount'].status == 'doc' and f['amount'].source.quote == 'USD 51,000.00'
    assert f['valueDate'].value == '2026-11-09' and f['valueDate'].status == 'ai' and '15:00' in f['valueDate'].note
    assert f['urgent'].value == 'false'
    assert f['beneficiaryIban'].value == 'AE980710123456789012345' and f['beneficiaryIban'].status == 'doc'
    assert f['beneficiarySwift'].value == 'DPBLAEAD' and f['beneficiaryName'].value == 'Gulf Horizon Machinery FZE'
    assert f['intermediaryBank'].status == 'doc' and 'NHTRUS33' in f['intermediaryBank'].value
    assert f['knp'].value == '711' and f['knp'].status == 'ai' and {o.value for o in f['knp'].options} >= {'710', '711'}
    assert 'Национального Банка' in f['knp'].note
    assert f['purpose'].value == ('PREPAYMENT FOR INDUSTRIAL EQUIPMENT INV GHM-INV-0412 DD 02.10.2026 '
                                  'CONTRACT AI-DXB-2026/014 DD 15.09.2026')
    assert f['linkContract'].status == 'conflict' and f['linkContract'].value == ''
    assert f['docNumber'].value == 'GHM-INV-0412' and f['docDate'].value == '2026-10-02' and f['docDate'].status == 'doc'
    assert f['attachments'].value == 'contract.docx; invoice.pdf'
    assert f['residenceCountry'].value == 'AE' == f['beneficiaryCountry'].value
    assert f['kbe'].value == '27' and f['kbe'].status == 'ai' and 'сверить' in f['kbe'].note
    assert f['addressType'].value == 'legal'
    assert f['feeType'].value == 'OUR' and f['feeType'].status == 'ai' and 'недоплата' in f['feeType'].note
    assert [o.value for o in f['feeType'].options] == ['OUR', 'SHA', 'BEN'] and '50 960,00' in f['feeType'].options[1].why
    assert f['feeAccount'].value == 'acc-usd'
    assert f['signerHead'].value == 'Сейткали Нурлан Ерланович'
    assert f['signerAccountant'].value == 'Не предусмотрен' and f['signerAccountant'].status == 'system'
    assert f['useBonuses'].value == 'false'
    assert f['acceptTerms'].value == 'false' and f['acceptTerms'].status == 'empty'
    for st in f.values():
        assert st.suggested is not None


def test_conflict_on_substitution_kit_c():
    f = check_payment(make_case('C')).form
    ib = f['beneficiaryIban']
    assert ib.status == 'conflict' and ib.value == ''
    assert [o.value for o in ib.options] == ['AE980710123456789012345', 'GE47KV0000000483920175']
    assert all(o.why for o in ib.options)
    assert f['beneficiarySwift'].status == 'conflict'
    assert f['beneficiaryName'].status == 'doc'
    assert f['linkContract'].value == '02/2026/00731' and f['linkContract'].status == 'system'
    assert f['purpose'].value.endswith('UN 02/2026/00731') and len(f['purpose'].value) <= 140


def test_conflicts_kit_e_and_d():
    f = check_payment(make_case('E')).form
    assert f['beneficiaryIban'].status == 'conflict' and f['beneficiaryIban'].options[0].value == 'AE980710123456789012345'
    assert f['beneficiaryName'].status == 'conflict'
    assert [o.value for o in f['beneficiaryName'].options] == ['Gulf Horizon Machinery FZE', 'Gulf Horizon Machinery Trading LLC']
    d = check_payment(make_case('D')).form
    assert d['amount'].status == 'conflict' and d['amount'].suggested == '21000.00'
    assert [o.value for o in d['amount'].options] == ['21000.00', '25000.00']


def test_kit_b_no_eur_account():
    f = check_payment(make_case('B')).form
    assert f['debitAccount'].status == 'conflict' and f['debitAccount'].value == '' and 'конвертац' in f['debitAccount'].note
    assert 'RHNBDEFF' in f['intermediaryBank'].value


def test_user_fields_preserved_and_confirmed():
    prev = {'amount': FieldState(value='50000.00', status='user', confirmed=True),
            'beneficiarySwift': FieldState(value='DPBLAEAD', status='doc', confirmed=True),
            'debitAccount': FieldState(value='acc-kzt', status='ai', confirmed=True),
            'acceptTerms': FieldState(value='true', status='doc', confirmed=True)}
    rep = check_payment(make_case('A', form=prev))
    f = rep.form
    assert f['amount'].value == '50000.00' and f['amount'].status == 'user' and f['amount'].suggested == '51000.00'
    assert f['beneficiarySwift'].confirmed is True
    assert f['debitAccount'].value == 'acc-usd' and f['debitAccount'].confirmed is False
    assert f['acceptTerms'].value == 'false' and f['acceptTerms'].status == 'empty'
    assert rep.fees['OUR'].sender_total == 50000 + 100 + 30


def test_task_timing_postpay_and_services():
    case = make_case('fixed', task={'timing': 'postpay'})
    f = check_payment(case).form
    assert f['knp'].value == '710' and f['purpose'].value.startswith('PAYMENT FOR')
    assert f['feeType'].value == 'SHA' and f['feeType'].status == 'doc'   # пункт 4.7 исправленного договора
    assert f['feeType'].source.quote.startswith('4.7.')
    assert check_payment(make_case('fixed', task={'goods': 'services'})).form['knp'].value == '780'


def test_build_form_direct_without_contract():
    case = make_case('A')
    case.docs = [case.docs[1]]
    exts = [extract_fields(d) for d in case.docs]
    from app.models import ContractSummary
    f = build_form(case, exts, [], ContractSummary())
    assert len(f) == 25 and f['beneficiaryIban'].status == 'doc' and f['linkContract'].status == 'empty'

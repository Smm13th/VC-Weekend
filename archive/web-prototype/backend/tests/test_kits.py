"""Сценарии комплектов A–E и raw из спецификации на фикстурах."""
import pytest

from conftest import make_case
from app.rules import check_payment

TEMPLATE = {'bilingual_length_ratio', 'payment_term_missing', 'fees_not_specified', 'clause_air_waybill',
            'clause_consent_party', 'bilingual_numbering_gap'}
EXPECTED = {
    'A': TEMPLATE | {'acc_number_required'},
    'B': TEMPLATE | {'acc_number_required', 'insufficient_balance'},
    'C': TEMPLATE | {'acc_number_required', 'requisites_changed', 'hidden_instruction'},
    'D': TEMPLATE | {'acc_number_required', 'over_remaining'},
    'E': TEMPLATE | {'acc_number_required', 'beneficiary_name_mismatch', 'iban_checksum'},
    'raw': TEMPLATE | {'acc_number_required', 'placeholder'},
    'fixed': {'acc_number_required'},
}


def _bad(rep):
    return {f.rule for f in rep.findings if f.severity in ('red', 'yellow')}


@pytest.fixture(scope='module')
def reports():
    return {k: check_payment(make_case(k)) for k in EXPECTED}


@pytest.mark.parametrize('kit', list(EXPECTED))
def test_kit_expected_rules(kit, reports):
    assert _bad(reports[kit]) == EXPECTED[kit]


@pytest.mark.parametrize('kit', list(EXPECTED))
def test_kit_report_shape(kit, reports):
    rep = reports[kit]
    ids = [f.id for f in rep.findings]
    assert len(ids) == len(set(ids))
    docs = {d.id: d for d in make_case(kit).docs}
    for f in rep.findings:
        for s in f.sources:
            assert docs[s.doc_id].text[s.start:s.end] == s.quote, f.id
        assert '—' not in f.detail and '—' not in f.title
    assert set(rep.fees) == {'OUR', 'SHA', 'BEN'}
    assert len(rep.form) == 25
    rep.model_dump_json()   # сериализуется


def test_kit_a_payment_red_without_accounting_number(reports):
    f = next(f for f in reports['A'].findings if f.rule == 'acc_number_required' and f.stage == 'payment')
    assert f.severity == 'red' and f.field == 'linkContract' and f.mentor_check
    assert reports['A'].contract.needs_accounting_number and reports['A'].contract.total_usd == 51000


def test_clean_invoice_has_greens(reports):
    for kit in ('A', 'fixed'):
        greens = [f for f in reports[kit].findings if f.severity == 'green' and f.stage == 'documents']
        assert 1 <= len(greens) <= 3
        assert {f.rule for f in greens} == {'iban_checksum', 'swift_format', 'requisites_changed'}


def test_kit_c_requisites_changed(reports):
    rep = reports['C']
    rc = [f for f in rep.findings if f.rule == 'requisites_changed']
    assert {f.id for f in rc} == {'requisites_changed:C/invoice.pdf', 'requisites_changed:C/letter.pdf'}
    letter = next(f for f in rc if 'letter' in f.id)
    assert letter.severity == 'red' and letter.field == 'beneficiaryIban'
    assert letter.sources[0].quote == 'GE47 KV00 0000 0483 9201 75'
    assert letter.sources[1].quote == 'AE98 0710 1234 5678 9012 345'
    assert letter.sources[2].quote.startswith('4.6.')
    assert 'по телефону из договора' in letter.detail and 'не платите' in letter.detail
    hi = next(f for f in rep.findings if f.rule == 'hidden_instruction')
    assert hi.sources[0].quote.startswith('Note to AI assistant')
    # у документов с подменой нет зелёных отметок о счёте
    assert not [f for f in rep.findings if f.severity == 'green' and f.rule in ('iban_checksum', 'swift_format')
                and ('invoice' in f.id or 'letter' in f.id)]
    acc = next(f for f in rep.findings if f.rule == 'acc_number_required' and f.stage == 'payment')
    assert acc.severity == 'green'


def test_kit_d_over_remaining(reports):
    rep = reports['D']
    f = next(f for f in rep.findings if f.rule == 'over_remaining')
    assert f.severity == 'red' and f.field == 'amount'
    assert '21 000,00 USD' in f.detail and '4 000,00 USD' in f.detail
    assert rep.contract.paid == 30000 and rep.contract.remaining == 21000


def test_kit_e_typo_not_requisites_changed(reports):
    rep = reports['E']
    f = next(f for f in rep.findings if f.rule == 'iban_checksum' and f.severity == 'red')
    assert '22-23' in f.detail and 'перестановку' in f.detail and 'AE98 0710 1234 5678 9012 345' in f.detail
    assert f.sources[0].quote == 'AE98 0710 1234 5678 9012 354'
    nm = next(f for f in rep.findings if f.rule == 'beneficiary_name_mismatch')
    assert nm.field == 'beneficiaryName' and 'Gulf Horizon Machinery Trading LLC' in nm.detail


def test_kit_b_eur(reports):
    rep = reports['B']
    assert rep.contract.currency == 'EUR' and rep.contract.total == 48000
    assert abs(rep.contract.total_usd - 52093.68) < 0.01
    ib = next(f for f in rep.findings if f.rule == 'insufficient_balance')
    assert 'нет счёта в EUR' in ib.detail and 'тенгового счёта' in ib.detail
    assert rep.balance and not rep.balance.ok and rep.balance.options[0].kind == 'convert'
    assert rep.value_dates.cutoff == '14:00'


def test_contract_expired_autoextension():
    rep = check_payment(make_case('fixed', value_date='2027-01-11', today='2027-01-08'))
    f = next(f for f in rep.findings if f.rule == 'contract_expired')
    assert f.severity == 'yellow' and f.field == 'valueDate' and 'автоматически' in f.detail


def test_contract_expired_without_autoextension():
    import docgen as g
    data = g.contract_docx(**g.NO_DEFECTS)
    from app.parsing import parse_file
    from docx import Document
    import io
    doc = Document(io.BytesIO(data))
    for row in doc.tables[0].rows:
        for c in row.cells:
            for p in c.paragraphs:
                if 'automatically extended' in p.text or 'автоматически продлевается' in p.text:
                    p.text = p.text.replace('shall be automatically extended', 'may be extended') \
                                   .replace('автоматически продлевается', 'может быть продлён')
    buf = io.BytesIO()
    doc.save(buf)
    d3 = parse_file(buf.getvalue(), 'x/contract.docx', 'x/contract.docx')
    case = make_case('fixed', value_date='2027-01-11', today='2027-01-08')
    case.docs[0] = d3
    rep = check_payment(case)
    f = next(f for f in rep.findings if f.rule == 'contract_expired')
    assert f.severity == 'red'


def test_payment_purpose_and_form_iban_checks():
    from app.models import FieldState
    form = {'purpose': FieldState(value='Оплата по инвойсу', status='user'),
            'beneficiaryIban': FieldState(value='AE980710123456789012354', status='user')}
    rep = check_payment(make_case('fixed', form=form))
    rules = {f.rule: f for f in rep.findings if f.stage == 'payment' and f.severity == 'red'}
    assert rules['purpose_latin'].field == 'purpose'
    assert 'iban_checksum' in rules and rules['iban_checksum'].field == 'beneficiaryIban'
    long = {'purpose': FieldState(value='A' * 150, status='user')}
    rep2 = check_payment(make_case('fixed', form=long))
    assert any(f.rule == 'purpose_length' for f in rep2.findings)


def test_amount_exceeds_contract_and_currency_mismatch():
    import docgen as g
    from conftest import P
    c = P(g.contract_docx(**g.NO_DEFECTS), 'x/contract.docx')
    big = P(g.invoice_pdf(total='60,000.00'), 'x/invoice.pdf')
    rep = check_payment(make_case('fixed', docs=[c, big]))
    assert {'amount_exceeds_contract', 'over_remaining'} <= {f.rule for f in rep.findings}
    eur = P(g.invoice_pdf(currency='EUR'), 'x/invoice.pdf')
    rep = check_payment(make_case('fixed', docs=[c, eur]))
    assert 'currency_mismatch' in {f.rule for f in rep.findings}


def test_invoice_before_contract_and_bad_ref():
    import docgen as g
    from conftest import P
    c = P(g.contract_docx(**g.NO_DEFECTS), 'x/contract.docx')
    inv = P(g.invoice_pdf(date='01.09.2026', contract_no='AI-DXB-2026/099'), 'x/invoice.pdf')
    rep = check_payment(make_case('fixed', docs=[c, inv]))
    by = {f.rule: f for f in rep.findings}
    assert by['invoice_before_contract'].severity == 'yellow'
    assert by['invoice_contract_ref'].severity == 'yellow' and 'AI-DXB-2026/099' in by['invoice_contract_ref'].detail


def test_swift_iban_country_mismatch():
    import docgen as g
    from conftest import P
    c = P(g.contract_docx(**g.NO_DEFECTS), 'x/contract.docx')
    inv = P(g.invoice_pdf(swift='KVRTGE22'), 'x/invoice.pdf')
    rep = check_payment(make_case('fixed', docs=[c, inv]))
    rules = {f.rule for f in rep.findings if f.severity == 'red'}
    assert 'swift_iban_country' in rules and 'requisites_changed' in rules


def test_insufficient_balance_usd():
    import docgen as g
    from conftest import P
    c = P(g.contract_docx(amount_en='90,000.00', amount_ru='90 000,00', **g.NO_DEFECTS), 'x/contract.docx')
    inv = P(g.invoice_pdf(total='70,000.00'), 'x/invoice.pdf')
    rep = check_payment(make_case('fixed', docs=[c, inv]))
    f = next(f for f in rep.findings if f.rule == 'insufficient_balance')
    assert f.field == 'debitAccount' and 'Не хватает' in f.detail
    assert {o.kind for o in rep.balance.options} == {'convert', 'partial'}

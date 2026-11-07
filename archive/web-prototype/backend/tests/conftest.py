import sys
from pathlib import Path

import pytest

HERE = Path(__file__).parent
sys.path.insert(0, str(HERE.parent))
sys.path.insert(0, str(HERE))

import docgen as g  # noqa: E402
from app.models import CaseInput, Payment  # noqa: E402
from app.parsing import parse_file  # noqa: E402


def P(data: bytes, name: str):
    return parse_file(data, name, name)


B_CONTRACT = dict(no='AI-DXB-2026/021', date='01.10.2026', expiry='31.12.2027', currency='EUR', amount_en='48,000.00',
                  amount_ru='48 000,00', words_en='forty-eight thousand euro 00 cents',
                  words_ru='сорок восемь тысяч евро 00 центов', cur_name_en='the Euro', cur_name_ru='евро')


def kit_docs(kit: str):
    c = lambda: P(g.contract_docx(), f'{kit}/contract.docx')  # noqa: E731
    if kit == 'A':
        return dict(docs=[c(), P(g.invoice_pdf(), 'A/invoice.pdf')])
    if kit == 'B':
        return dict(docs=[P(g.contract_docx(**B_CONTRACT), 'B/contract.docx'),
                          P(g.invoice_docx(no='GHM-INV-0450', date='08.10.2026', contract_no='AI-DXB-2026/021',
                                           contract_date='01.10.2026', currency='EUR', total='48,000.00', due='18.10.2026',
                                           intermediary='Rhein Nord Bank AG, Frankfurt am Main, SWIFT RHNBDEFF',
                                           items=[('Hydraulic press HP-250 with control unit', '1', '48,000.00', '48,000.00')]),
                            'B/invoice.docx')])
    if kit == 'C':
        return dict(docs=[c(), P(g.invoice_pdf(no='GHM-INV-0431', date='22.10.2026', bank='Kavkaz Trade Bank JSC, Tbilisi, Georgia',
                                               iban=g.FRAUD_IBAN, swift='KVRTGE22', due='01.11.2026'), 'C/invoice.pdf'),
                          P(g.letter_pdf(), 'C/letter.pdf')], accounting_number='02/2026/00731')
    if kit == 'D':
        return dict(docs=[c(), P(g.invoice_pdf(no='GHM-INV-0440', date='25.10.2026', total='25,000.00',
                                               items=[('Hydraulic press HP-250 with control unit', '1', '25,000.00', '25,000.00')]),
                                 'D/invoice.pdf')],
                    accounting_number='02/2026/00731',
                    payments=[Payment(date='2026-10-05', amount=30000, currency='USD', ref='Invoice GHM-INV-0398')])
    if kit == 'E':
        return dict(docs=[c(), P(g.invoice_pdf(no='GHM-INV-0415', date='06.10.2026', beneficiary='Gulf Horizon Machinery Trading LLC',
                                               iban=g.TYPO_IBAN), 'E/invoice.pdf')], accounting_number='02/2026/00731')
    if kit == 'raw':
        return dict(docs=[P(g.contract_docx(**g.RAW), 'raw/contract.docx')])
    if kit == 'fixed':
        return dict(docs=[P(g.contract_docx(**g.NO_DEFECTS), 'F/contract.docx'), P(g.invoice_pdf(), 'F/invoice.pdf')],
                    accounting_number='02/2026/00731')
    raise KeyError(kit)


def make_case(kit: str, **kw) -> CaseInput:
    base = dict(value_date='2026-11-09', today='2026-11-07')
    base.update(kit_docs(kit))
    base.update(kw)
    return CaseInput(**base)


@pytest.fixture(scope='session')
def contract_defective():
    return P(g.contract_docx(), 'A/contract.docx')


@pytest.fixture(scope='session')
def contract_fixed():
    return P(g.contract_docx(**g.NO_DEFECTS), 'F/contract.docx')


@pytest.fixture(scope='session')
def invoice_pdf_doc():
    return P(g.invoice_pdf(split_page_after=9), 'A/invoice.pdf')


@pytest.fixture(scope='session')
def letter_doc():
    return P(g.letter_pdf(), 'C/letter.pdf')

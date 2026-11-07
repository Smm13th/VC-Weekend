"""Pydantic-модели API. Это контракт между бэкендом и фронтендом (frontend/src/types.ts).
В Python атрибуты snake_case, в JSON camelCase (alias). Значения-идентификаторы (FieldId, RuleId, ExtractKey) — строки как есть.
Все позиции start/end — индексы в ParsedDoc.text, end не включается.
"""
from __future__ import annotations

from typing import Literal, Optional

from pydantic import BaseModel, ConfigDict, Field
from pydantic.alias_generators import to_camel


class Model(BaseModel):
    model_config = ConfigDict(alias_generator=to_camel, populate_by_name=True, serialize_by_alias=True)


DocKind = Literal['contract', 'invoice', 'letter', 'other']
Lang = Literal['en', 'ru']
Severity = Literal['red', 'yellow', 'green']
FindingStage = Literal['contract', 'documents', 'payment']
FieldStatus = Literal['doc', 'ai', 'conflict', 'user', 'system', 'empty']
FeeType = Literal['OUR', 'SHA', 'BEN']

RULE_IDS = [
    'placeholder', 'iban_format', 'iban_checksum', 'swift_format', 'swift_iban_country',
    'acc_number_required', 'over_remaining', 'amount_exceeds_contract', 'currency_mismatch',
    'invoice_before_contract', 'contract_expired', 'invoice_contract_ref', 'requisites_changed',
    'beneficiary_name_mismatch', 'hidden_instruction', 'bilingual_numbering_gap',
    'bilingual_number_mismatch', 'bilingual_length_ratio', 'clause_consent_party', 'clause_air_waybill',
    'fees_not_specified', 'payment_term_missing', 'purpose_latin', 'purpose_length', 'insufficient_balance',
]
RuleId = Literal[
    'placeholder', 'iban_format', 'iban_checksum', 'swift_format', 'swift_iban_country',
    'acc_number_required', 'over_remaining', 'amount_exceeds_contract', 'currency_mismatch',
    'invoice_before_contract', 'contract_expired', 'invoice_contract_ref', 'requisites_changed',
    'beneficiary_name_mismatch', 'hidden_instruction', 'bilingual_numbering_gap',
    'bilingual_number_mismatch', 'bilingual_length_ratio', 'clause_consent_party', 'clause_air_waybill',
    'fees_not_specified', 'payment_term_missing', 'purpose_latin', 'purpose_length', 'insufficient_balance',
]

EXTRACT_KEYS = [
    'contractNo', 'contractDate', 'expiryDate', 'currency', 'totalAmount',
    'sellerName', 'sellerRepresentative', 'sellerAddress', 'sellerCountry', 'sellerLicenseNo',
    'sellerBank', 'sellerBankAddress', 'sellerAccountName', 'sellerAccountNo', 'sellerIban', 'sellerSwift',
    'buyerName', 'buyerAddress', 'buyerIban', 'buyerBic',
    'invoiceNo', 'invoiceDate', 'invoiceAmount', 'invoiceDueDate', 'invoiceContractRef',
    'goodsDescription', 'deliveryTerms', 'paymentTerms',
]

FIELD_IDS = [
    'requestNo', 'debitAccount', 'amount', 'valueDate', 'urgent',
    'beneficiarySwift', 'intermediaryBank', 'knp', 'purpose', 'linkContract',
    'docNumber', 'docDate', 'attachments', 'beneficiaryName', 'residenceCountry',
    'beneficiaryIban', 'kbe', 'beneficiaryCountry', 'addressType', 'feeType',
    'feeAccount', 'signerHead', 'signerAccountant', 'useBonuses', 'acceptTerms',
]


class DocBlock(Model):
    """Фрагмент текста. Для двуязычной таблицы договора: row = номер строки таблицы, lang = колонка."""
    start: int
    end: int
    row: Optional[int] = None
    lang: Optional[Lang] = None
    page: Optional[int] = None   # номер страницы PDF, с 1


class ParsedDoc(Model):
    id: str                      # стабильный id, например 'A/contract.docx'
    name: str
    kind: DocKind
    format: Literal['docx', 'pdf', 'txt']
    text: str                    # весь текст; блоки соединены '\n'
    blocks: list[DocBlock]
    pages: int = 1


class SourceRef(Model):
    doc_id: str
    start: int
    end: int
    quote: str                   # == doc.text[start:end]
    page: Optional[int] = None


class Extracted(Model):
    key: str                     # ExtractKey
    value: str                   # как в документе
    norm: Optional[str | float] = None   # даты 'YYYY-MM-DD', суммы float, IBAN/SWIFT без пробелов в верхнем регистре
    source: SourceRef
    confidence: Literal['high', 'medium', 'low'] = 'high'
    status: Literal['doc', 'conflict'] = 'doc'   # conflict: в документе несколько разных значений
    note: Optional[str] = None


class ExtractionResult(Model):
    doc_id: str
    kind: DocKind
    fields: dict[str, Extracted] = Field(default_factory=dict)   # ключ ExtractKey


class Suggestion(Model):
    ru: str
    en: str
    clause: Optional[str] = None


class Finding(Model):
    id: str                      # уникальный в кейсе: '<rule>:<docId>[:доп]'
    rule: RuleId
    stage: FindingStage
    severity: Severity           # red блокирует, yellow обсудить с поставщиком, green ок
    title: str                   # коротко по-русски
    detail: str                  # простым языком, без сокращений без расшифровки
    sources: list[SourceRef] = Field(default_factory=list)
    field: Optional[str] = None  # FieldId
    suggestion: Optional[Suggestion] = None
    mentor_check: bool = False   # «сверить с менторами/справочником»


class ClausePair(Model):
    num: str                     # '3.1', '12.7'; заголовок статьи '3'
    en: Optional[SourceRef] = None
    ru: Optional[SourceRef] = None
    ratio: Optional[float] = None    # len(ru)/len(en)
    numbers_en: list[str] = Field(default_factory=list)
    numbers_ru: list[str] = Field(default_factory=list)
    issues: list[str] = Field(default_factory=list)   # RuleId


class FieldOption(Model):
    value: str
    label: str
    why: str


class FieldState(Model):
    value: str = ''              # чекбоксы 'true'/'false'
    status: FieldStatus = 'empty'
    source: Optional[SourceRef] = None
    confirmed: bool = False
    note: Optional[str] = None   # почему такое значение (для кнопки «Почему?»)
    options: Optional[list[FieldOption]] = None
    suggested: Optional[str] = None   # что предлагал помощник


class Account(Model):
    id: str
    iban: str
    currency: str
    balance: float
    title: str


class Payment(Model):
    date: str                    # YYYY-MM-DD
    amount: float
    currency: str
    ref: str


class CaseInput(Model):
    docs: list[ParsedDoc]
    payments: list[Payment] = Field(default_factory=list)
    accounting_number: Optional[str] = None
    value_date: str              # YYYY-MM-DD
    today: Optional[str] = None  # YYYY-MM-DD, по умолчанию системная дата
    form: Optional[dict[str, FieldState]] = None   # ключ FieldId; поля со status 'user' не перезаписываются
    task: Optional[dict[str, str]] = None          # ответы со стартового экрана, например {'goods': 'goods', 'timing': 'prepay'}


class FeeLine(Model):
    who: Literal['sender', 'beneficiary']
    label: str
    amount: float


class FeeSim(Model):
    fee_type: FeeType
    currency: str
    sender_total: float          # списано со счёта: сумма + комиссии отправителя
    sender_fees: float
    receiver_gets: float
    shortfall: float             # недополучит получатель относительно суммы перевода
    urgent_fee_kzt: float
    lines: list[FeeLine] = Field(default_factory=list)


class BalanceOption(Model):
    kind: Literal['convert', 'partial']
    title: str
    detail: str
    from_account_id: Optional[str] = None
    amount: Optional[float] = None


class BalanceCheck(Model):
    ok: bool
    currency: str
    available: float
    needed: float
    shortfall: float
    options: list[BalanceOption] = Field(default_factory=list)


class ContractSummary(Model):
    contract_no: Optional[str] = None
    contract_date: Optional[str] = None
    currency: Optional[str] = None
    total: Optional[float] = None
    total_usd: Optional[float] = None
    paid: float = 0
    remaining: Optional[float] = None
    needs_accounting_number: bool = False
    threshold_usd: float = 50000


class ValueDates(Model):
    standard: str                # YYYY-MM-DD
    urgent: str
    cutoff: str                  # 'HH:MM'


class KbeInfo(Model):
    code: str
    why: str
    mentor_check: bool = True


class DocumentResult(Model):
    doc: ParsedDoc
    extraction: ExtractionResult
    clauses: list[ClausePair] = Field(default_factory=list)


class ContractReport(Model):
    findings: list[Finding]
    clauses: list[ClausePair]
    extraction: ExtractionResult


class CheckContractRequest(Model):
    doc: ParsedDoc


class PaymentReport(Model):
    findings: list[Finding]      # все этапы: contract, documents, payment
    form: dict[str, FieldState]  # 25 полей, ключ FieldId
    extractions: list[ExtractionResult]
    contract: ContractSummary
    fees: dict[str, FeeSim]      # 'OUR' | 'SHA' | 'BEN'
    balance: Optional[BalanceCheck] = None
    value_dates: ValueDates
    kbe: Optional[KbeInfo] = None

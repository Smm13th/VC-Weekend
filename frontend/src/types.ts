// Общие типы и контракты модулей ВЭД-помощника.
// Все позиции (start/end) — индексы в ParsedDoc.text, end не включается.

export type DocKind = 'contract' | 'invoice' | 'letter' | 'other'
export type Lang = 'en' | 'ru'

/** Фрагмент текста документа. Для двуязычной таблицы договора: row = номер строки таблицы, lang = колонка. */
export interface DocBlock {
  start: number
  end: number
  row?: number
  lang?: Lang
}

export interface ParsedDoc {
  id: string            // стабильный id, например 'A/contract.docx'
  name: string          // имя файла для показа
  kind: DocKind
  format: 'docx' | 'pdf' | 'txt'
  text: string          // весь текст; блоки соединены '\n'
  blocks: DocBlock[]    // по порядку, покрывают text
}

/** Ссылка на точное место в документе: из него показывается цитата и подсветка. */
export interface SourceRef {
  docId: string
  start: number
  end: number
  quote: string         // === doc.text.slice(start, end)
}

export type ExtractKey =
  | 'contractNo' | 'contractDate' | 'expiryDate' | 'currency' | 'totalAmount'
  | 'sellerName' | 'sellerRepresentative' | 'sellerAddress' | 'sellerCountry' | 'sellerLicenseNo'
  | 'sellerBank' | 'sellerBankAddress' | 'sellerAccountName' | 'sellerAccountNo' | 'sellerIban' | 'sellerSwift'
  | 'buyerName' | 'buyerAddress' | 'buyerIban' | 'buyerBic'
  | 'invoiceNo' | 'invoiceDate' | 'invoiceAmount' | 'invoiceDueDate' | 'invoiceContractRef'
  | 'goodsDescription' | 'deliveryTerms' | 'paymentTerms'

export interface Extracted {
  key: ExtractKey
  value: string                 // как в документе, без лишних пробелов
  norm?: string | number        // нормализованное: даты 'YYYY-MM-DD', суммы number, IBAN без пробелов, верхний регистр
  source: SourceRef
  confidence: 'high' | 'medium' | 'low'
}

export interface ExtractionResult {
  docId: string
  kind: DocKind
  fields: Partial<Record<ExtractKey, Extracted>>
}

export type Severity = 'red' | 'yellow' | 'green'
export type FindingStage = 'contract' | 'documents' | 'payment'

/** Канонические идентификаторы правил. Набор для оценки (eval/cases.json) ссылается на них. */
export type RuleId =
  | 'placeholder'               // незаполненные [..] в документе
  | 'iban_format'               // IBAN неверной длины/структуры для страны
  | 'iban_checksum'             // IBAN не проходит mod 97
  | 'swift_format'              // SWIFT/BIC не соответствует формату
  | 'swift_iban_country'        // страна в SWIFT (5-6) и IBAN (1-2) различаются
  | 'acc_number_required'       // импорт > экв. 50 000 USD, учётного номера нет
  | 'over_remaining'            // сумма к оплате больше остатка по договору
  | 'amount_exceeds_contract'   // сумма инвойса больше общей суммы договора
  | 'currency_mismatch'         // валюта инвойса отличается от валюты договора
  | 'invoice_before_contract'   // дата инвойса раньше даты договора
  | 'contract_expired'          // срок договора истёк к дате валютирования
  | 'invoice_contract_ref'      // инвойс не ссылается на номер/дату договора или ссылается на другой
  | 'requisites_changed'        // IBAN/SWIFT в инвойсе или письме отличаются от договора (п. 4.6)
  | 'beneficiary_name_mismatch' // наименование получателя в инвойсе отличается от договора
  | 'hidden_instruction'        // скрытые инструкции для ИИ в тексте документа
  | 'bilingual_numbering_gap'   // пропуск в нумерации пунктов (12.6)
  | 'bilingual_number_mismatch' // числа/даты в паре пунктов EN/RU не совпадают
  | 'bilingual_length_ratio'    // аномальное соотношение длины RU/EN (лишняя фраза в 3.1)
  | 'clause_consent_party'      // 8.2: согласие не той стороны
  | 'clause_air_waybill'        // 5.4: авиадоставка и Bill of Lading
  | 'fees_not_specified'        // в договоре не сказано, кто платит банковские комиссии
  | 'payment_term_missing'      // в договоре нет явного срока оплаты
  | 'purpose_latin'             // назначение не латиницей
  | 'purpose_length'            // назначение длиннее 140 символов
  | 'insufficient_balance'      // на счёте списания не хватает денег

export interface Finding {
  id: string                    // уникальный в рамках кейса, например 'iban_checksum:E/invoice.pdf'
  rule: RuleId
  stage: FindingStage
  severity: Severity            // red блокирует, yellow обсудить с поставщиком, green ок
  title: string                 // коротко, по-русски
  detail: string                // объяснение простым языком, без сокращений без расшифровки
  sources: SourceRef[]          // клик по находке подсвечивает первую ссылку
  field?: FieldId               // поле заявки, к которому относится
  suggestion?: { ru: string; en: string; clause?: string }  // предложенная формулировка пункта
  mentorCheck?: boolean         // пометка «сверить с менторами/справочником»
}

/** Поля заявки «Валютный перевод в другой банк» в порядке памятки (1..25). */
export type FieldId =
  | 'requestNo' | 'debitAccount' | 'amount' | 'valueDate' | 'urgent'
  | 'beneficiarySwift' | 'intermediaryBank' | 'knp' | 'purpose' | 'linkContract'
  | 'docNumber' | 'docDate' | 'attachments' | 'beneficiaryName' | 'residenceCountry'
  | 'beneficiaryIban' | 'kbe' | 'beneficiaryCountry' | 'addressType' | 'feeType'
  | 'feeAccount' | 'signerHead' | 'signerAccountant' | 'useBonuses' | 'acceptTerms'

/**
 * doc — взято из документа (есть source), ai — догадка помощника, нужно подтвердить,
 * conflict — не найдено или противоречие (красное), user — введено/исправлено пользователем,
 * system — заполняет система (номер заявки), empty — пусто.
 */
export type FieldStatus = 'doc' | 'ai' | 'conflict' | 'user' | 'system' | 'empty'

export interface FieldOption { value: string; label: string; why: string }

export interface FieldState {
  value: string                 // для чекбоксов 'true' | 'false'
  status: FieldStatus
  source?: SourceRef
  confirmed: boolean            // пользователь подтвердил значение
  note?: string                 // пояснение «почему такое значение» для кнопки «Почему?»
  options?: FieldOption[]       // варианты (КНП, тип комиссии)
  suggested?: string            // что предлагал помощник (для журнала: принял/исправил)
}

export type FormState = Record<FieldId, FieldState>

export interface Account {
  id: string
  iban: string
  currency: 'USD' | 'KZT' | 'CNY' | 'RUB' | 'EUR'
  balance: number
  title: string
}

export interface Payment {
  date: string        // YYYY-MM-DD
  amount: number
  currency: string
  ref: string         // документ-основание
}

export type Rates = Record<string, number>   // сколько KZT за 1 единицу валюты, KZT = 1

export interface CaseInput {
  docs: ParsedDoc[]
  extractions: ExtractionResult[]
  payments: Payment[]
  accountingNumber: string | null
  valueDate: string                 // YYYY-MM-DD
  form?: FormState
  accounts: Account[]
  rates: Rates
}

export interface LogEntry {
  at: string                        // ISO
  actor: 'ai' | 'code' | 'user'
  action: 'suggested' | 'accepted' | 'edited' | 'rejected' | 'checked' | 'uploaded' | 'signed' | 'info'
  target: string                    // поле, находка или этап
  detail: string
}

export interface KitManifest {
  id: string                        // 'A'..'E', 'raw'
  title: string
  description: string               // что заложено в комплект
  files: string[]                   // пути относительно public/samples/
  payments: Payment[]               // уже оплачено по договору
  accountingNumber: string | null
  expected: RuleId[]                // какие правила должны сработать
}

export type AiMode = 'fallback' | 'webllm'
export type ModelState = 'idle' | 'loading' | 'ready' | 'error' | 'unsupported'

/** Пара пунктов двуязычного договора после выравнивания по номерам. */
export interface ClausePair {
  num: string                 // '3.1', '12.7', для заголовков статей '3'
  en?: SourceRef
  ru?: SourceRef
  ratio?: number              // длина RU / длина EN
  numbersEn: string[]         // нормализованные числа и даты из EN
  numbersRu: string[]
  issues: RuleId[]
}

export interface FeeLine { who: 'sender' | 'beneficiary'; label: string; amount: number }
export interface FeeSim {
  feeType: 'OUR' | 'SHA' | 'BEN'
  currency: string
  senderTotal: number         // списано со счёта клиента: сумма + комиссии отправителя (в валюте перевода)
  senderFees: number          // комиссии, которые платит отправитель
  receiverGets: number        // сколько дойдёт до получателя
  shortfall: number           // недополучит получатель относительно суммы перевода
  urgentFeeKzt: number
  lines: FeeLine[]
}

export interface BalanceOption {
  kind: 'convert' | 'partial'
  title: string
  detail: string
  fromAccountId?: string
  amount?: number             // сумма к конвертации (в валюте счёта-источника) или частичной оплаты
}
export interface BalanceCheck {
  ok: boolean
  currency: string
  available: number
  needed: number
  shortfall: number
  options: BalanceOption[]
}

/** Статусы платежа после подписи (имитация). */
export type StatusStage = 'draft' | 'signed' | 'accepted' | 'fx_control' | 'docs_requested' | 'executed' | 'returned'

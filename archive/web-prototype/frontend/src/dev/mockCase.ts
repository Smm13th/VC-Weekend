// Тестовые данные для разработки экранов, пока парсер и правила не готовы. В демо не используются.
import type { DocBlock, ExtractKey, ExtractionResult, Extracted, Finding, FormState, ParsedDoc, Payment, SourceRef } from '../types'
import { emptyForm } from '../lib/form/prefill'

function bilingual(id: string, name: string, rows: [string, string][]): ParsedDoc {
  let text = ''
  const blocks: DocBlock[] = []
  rows.forEach(([en, ru], row) => {
    for (const [lang, t] of [['en', en], ['ru', ru]] as const) {
      if (text) text += '\n'
      blocks.push({ start: text.length, end: text.length + t.length, row, lang })
      text += t
    }
  })
  return { id, name, kind: 'contract', format: 'docx', text, blocks }
}
function plain(id: string, name: string, kind: ParsedDoc['kind'], lines: string[]): ParsedDoc {
  let text = ''
  const blocks: DocBlock[] = []
  for (const l of lines) {
    if (text) text += '\n'
    blocks.push({ start: text.length, end: text.length + l.length })
    text += l
  }
  return { id, name, kind, format: 'pdf', text, blocks }
}
function ref(doc: ParsedDoc, quote: string): SourceRef {
  const start = doc.text.indexOf(quote)
  return { docId: doc.id, start, end: start + quote.length, quote }
}

export function mockCase() {
  const contract = bilingual('mock/contract.docx', 'contract.docx', [
    ['Contract\nRef. AI-DXB-2026/014\nDate: 15.09.2026', 'Договор\nRef. AI-DXB-2026/014\nДата: 15.09.2026'],
    ['Gulf Horizon Machinery FZE, hereinafter the Seller, represented by Manager Omar Haddad, and Alatau Industrial LLP, hereinafter the Buyer, represented by Director Nurlan Seitkali, acting on the basis of the Charter, have concluded this Contract as follows:', 'Gulf Horizon Machinery FZE, именуемое в дальнейшем Продавец, в лице менеджера Omar Haddad, и ТОО «Алатау Индастриал», именуемое в дальнейшем Покупатель, в лице директора Сейткали Н. Е., действующего на основании Устава, заключили настоящий Договор о нижеследующем:'],
    ['2.2. The total value of this Contract is USD 51,000.00 (fifty-one thousand US dollars 00 cents).', '2.2. Общая стоимость настоящего Договора составляет USD 51 000,00 (пятьдесят одна тысяча долларов США 00 центов).'],
    ['3.1. This Contract is valid until 31.12.2026 and shall be automatically extended for each subsequent calendar year unless either Party notifies the other of termination 2 months in advance.', '3.1. Настоящий Договор действует до 31.12.2026 и автоматически продлевается на каждый последующий календарный год, если ни одна из Сторон не заявит о прекращении за 2 месяца. Количество пролонгаций не ограничено.'],
    ['4.6. In case of a change of bank details or addresses, the Parties shall notify each other in advance within 10 calendar days.', '4.6. В случае изменения банковских реквизитов или адресов Стороны извещают друг друга заранее в течение 10 календарных дней.'],
    ['8.2. The Seller shall not disclose any information of the Buyer without the Seller\'s prior written consent.', '8.2. Продавец не разглашает информацию Покупателя без предварительного письменного согласия Продавца.'],
    ['Seller: Gulf Horizon Machinery FZE\nOffice Address: Office 1204, JAFZA One, Jebel Ali Free Zone, Dubai, UAE\nBeneficiary Bank: Desert Pearl Bank PJSC, Jebel Ali Branch\nAccount Name: Gulf Horizon Machinery FZE\nAccount No: 0123456789012345\nIBAN: AE98 0710 1234 5678 9012 345\nSwift Code: DPBLAEAD', 'Продавец: Gulf Horizon Machinery FZE\nАдрес офиса: Office 1204, JAFZA One, Jebel Ali Free Zone, Dubai, UAE\nБанк получателя: Desert Pearl Bank PJSC\nIBAN: AE98 0710 1234 5678 9012 345\nSWIFT: DPBLAEAD'],
  ])
  const invoice = plain('mock/invoice.pdf', 'invoice-0412.pdf', 'invoice', [
    'COMMERCIAL INVOICE',
    'Invoice No: GHM-INV-0412',
    'Date: 02.10.2026',
    'Contract Ref.: AI-DXB-2026/014 dated 15.09.2026',
    'Buyer: Alatau Industrial LLP, Almaty, Kazakhstan',
    'Hydraulic press HP-250, 1 pc, USD 51,000.00',
    'Total: USD 51,000.00',
    'Payment terms: 100% prepayment, due 12.10.2026',
    'Beneficiary: Gulf Horizon Machinery FZE',
    'IBAN: AE98 0710 1234 5678 9012 345',
    'Swift Code: DPBLAEAD',
  ])
  const ex = (doc: ParsedDoc, key: ExtractKey, quote: string, norm?: string | number): Extracted => ({ key, value: quote, norm, source: ref(doc, quote), confidence: 'high' })
  const extractions: ExtractionResult[] = [
    { docId: contract.id, kind: 'contract', fields: {
      contractNo: ex(contract, 'contractNo', 'AI-DXB-2026/014'), contractDate: ex(contract, 'contractDate', '15.09.2026', '2026-09-15'),
      totalAmount: ex(contract, 'totalAmount', 'USD 51,000.00', 51000), currency: ex(contract, 'currency', 'USD', 'USD'),
      sellerName: ex(contract, 'sellerName', 'Gulf Horizon Machinery FZE'), sellerIban: ex(contract, 'sellerIban', 'AE98 0710 1234 5678 9012 345', 'AE980710123456789012345'),
      sellerSwift: ex(contract, 'sellerSwift', 'DPBLAEAD', 'DPBLAEAD'), expiryDate: ex(contract, 'expiryDate', '31.12.2026', '2026-12-31'),
    } },
    { docId: invoice.id, kind: 'invoice', fields: {
      invoiceNo: ex(invoice, 'invoiceNo', 'GHM-INV-0412'), invoiceDate: ex(invoice, 'invoiceDate', '02.10.2026', '2026-10-02'),
      invoiceAmount: ex(invoice, 'invoiceAmount', 'USD 51,000.00', 51000), sellerIban: ex(invoice, 'sellerIban', 'AE98 0710 1234 5678 9012 345', 'AE980710123456789012345'),
    } },
  ]
  const findings: Finding[] = [
    { id: 'm1', rule: 'acc_number_required', stage: 'contract', severity: 'red', title: 'Нужен учётный номер договора', detail: 'Сумма договора 51 000 USD больше порога 50 000 USD в эквиваленте. До первого платежа договор нужно поставить на учёт и получить учётный номер.', sources: [ref(contract, 'USD 51,000.00')], mentorCheck: true, field: 'linkContract' },
    { id: 'm2', rule: 'bilingual_length_ratio', stage: 'contract', severity: 'yellow', title: 'В пункте 3.1 русский текст длиннее английского', detail: 'В русской версии есть фраза «Количество пролонгаций не ограничено», которой нет в английской. При расхождении приоритет у русского текста (п. 12.9).', sources: [ref(contract, 'Количество пролонгаций не ограничено.')], suggestion: { clause: '3.1', ru: 'Удалить фразу «Количество пролонгаций не ограничено» или добавить её в английский текст.', en: 'The number of extensions is not limited.' } },
    { id: 'm3', rule: 'clause_consent_party', stage: 'contract', severity: 'yellow', title: 'Пункт 8.2: согласие не той стороны', detail: 'Продавец не может разглашать информацию Покупателя без согласия самого Продавца, то есть защиты нет. Должно быть согласие Покупателя.', sources: [ref(contract, "without the Seller's prior written consent")], suggestion: { clause: '8.2', ru: 'Продавец не разглашает информацию Покупателя без предварительного письменного согласия Покупателя.', en: "The Seller shall not disclose any information of the Buyer without the Buyer's prior written consent." } },
    { id: 'm4', rule: 'fees_not_specified', stage: 'contract', severity: 'yellow', title: 'Не сказано, кто платит банковские комиссии', detail: 'Если выбрать тип комиссии SHA или BEN, продавец получит меньше 51 000 USD, и формально это недоплата.', sources: [ref(contract, '4.6.')], field: 'feeType' },
    { id: 'm5', rule: 'requisites_changed', stage: 'documents', severity: 'green', title: 'Реквизиты в инвойсе совпадают с договором', detail: 'IBAN и SWIFT в инвойсе такие же, как в статье 13 договора.', sources: [ref(invoice, 'AE98 0710 1234 5678 9012 345')] },
  ]
  const form: FormState = emptyForm()
  form.debitAccount = { value: 'acc-usd', status: 'ai', confirmed: false, note: 'Договор в долларах США, на долларовом счёте 64 300 USD.', suggested: 'acc-usd' }
  form.amount = { value: '51000.00', status: 'doc', source: extractions[1].fields.invoiceAmount!.source, confirmed: false, suggested: '51000.00' }
  form.beneficiaryIban = { value: 'AE980710123456789012345', status: 'doc', source: extractions[1].fields.sellerIban!.source, confirmed: false, suggested: 'AE980710123456789012345' }
  form.beneficiarySwift = { value: 'DPBLAEAD', status: 'doc', source: ref(invoice, 'DPBLAEAD'), confirmed: false, suggested: 'DPBLAEAD' }
  form.beneficiaryName = { value: 'Gulf Horizon Machinery FZE', status: 'doc', source: ref(invoice, 'Gulf Horizon Machinery FZE'), confirmed: false, suggested: 'Gulf Horizon Machinery FZE' }
  form.knp = { value: '711', status: 'ai', confirmed: false, note: 'Инвойс на предоплату, товар ещё не отгружен.', suggested: '711', options: [
    { value: '711', label: '711 Предоплата за импортируемые товары', why: 'В инвойсе написано 100% prepayment.' },
    { value: '710', label: '710 Оплата за импортированные товары', why: 'Подходит, если товар уже отгружен.' },
  ] }
  form.linkContract = { value: '', status: 'conflict', confirmed: false, note: 'Нужен учётный номер договора, его пока нет.' }
  const payments: Payment[] = []
  return { docs: [contract, invoice], extractions, findings, form, payments }
}

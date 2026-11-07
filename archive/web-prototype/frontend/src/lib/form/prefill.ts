// КОНТРАКТ (реализует исполнитель «парсинг+правила»). Сигнатуры не менять.
import type { CaseInput, FieldId, FieldState, FormState } from '../../types'
import fields from '../../data/fields.json'

export const FIELD_IDS = fields.map((f) => f.id) as FieldId[]

export function emptyForm(): FormState {
  const f = {} as FormState
  for (const id of FIELD_IDS) f[id] = { value: '', status: 'empty', confirmed: false }
  f.requestNo = { value: 'черновик', status: 'system', confirmed: true }
  f.signerAccountant = { value: 'Не предусмотрен', status: 'system', confirmed: true }
  f.acceptTerms = { value: 'false', status: 'empty', confirmed: false }
  return f
}

/**
 * Предзаполнение 25 полей из извлечённых данных и справочников.
 * Правила статусов: значение из документа -> 'doc' + source; вывод помощника (КНП, КБе, тип комиссии, назначение) -> 'ai';
 * нет данных или документы противоречат (например, разные IBAN в договоре и инвойсе) -> 'conflict' + note.
 * Поля со status 'user' в prev не перезаписываются. acceptTerms всегда 'false', его ставит только человек.
 */
export function buildForm(_input: CaseInput, prev?: FormState): FormState {
  return prev ?? emptyForm()
}

/** Логика доступности полей как в памятке: сумма и срочный перевод недоступны без счёта списания, КБе без страны резидентства и т.д. */
export function fieldAvailability(form: FormState): Record<FieldId, { enabled: boolean; reason?: string }> {
  const r = {} as Record<FieldId, { enabled: boolean; reason?: string }>
  for (const id of FIELD_IDS) r[id] = { enabled: true }
  if (!form.debitAccount.value) {
    r.amount = { enabled: false, reason: 'Сначала выберите счёт списания' }
    r.urgent = { enabled: false, reason: 'Сначала выберите счёт списания' }
  }
  if (!form.residenceCountry.value) r.kbe = { enabled: false, reason: 'Сначала выберите страну резидентства' }
  return r
}

export type { FieldState }

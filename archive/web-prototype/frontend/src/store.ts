import { create } from 'zustand'
import { persist, createJSONStorage } from 'zustand/middleware'
import type {
  Account, AiMode, CaseInput, ExtractionResult, FieldId, Finding, FormState, KitManifest,
  LogEntry, ModelState, ParsedDoc, Payment, SourceRef, StatusStage,
} from './types'
import { parseFile } from './lib/parse'
import { extractFields } from './lib/extract'
import { runChecks } from './lib/rules'
import { buildForm, emptyForm } from './lib/form/prefill'
import accountsData from './data/accounts.json'
import ratesData from './data/rates.json'
import fieldsMeta from './data/fields.json'

export const ACCOUNTS = accountsData.accounts as Account[]
export const CLIENT = accountsData.client
export const RATES = ratesData.rates as Record<string, number>
export const FIELD_LABEL = Object.fromEntries(fieldsMeta.map((f) => [f.id, f.label])) as Record<FieldId, string>
export const SAMPLES_BASE = `${import.meta.env.BASE_URL}samples/`

/** Сегодня в прототипе: системная дата. Дата валютирования по умолчанию: следующий рабочий день. */
export function todayIso(): string { return new Date().toISOString().slice(0, 10) }
function nextBusinessDay(iso: string): string {
  const d = new Date(iso + 'T00:00:00Z')
  do { d.setUTCDate(d.getUTCDate() + 1) } while (d.getUTCDay() === 0 || d.getUTCDay() === 6)
  return d.toISOString().slice(0, 10)
}

export interface TaskInfo { text: string; answers: Record<string, string>; createdAt: string | null }
export interface StatusEvent { stage: StatusStage; at: string }

interface AppState {
  kitId: string | null
  mock: boolean
  docs: ParsedDoc[]
  extractions: ExtractionResult[]
  findings: Finding[]
  payments: Payment[]
  accountingNumber: string | null
  registrationSubmittedAt: string | null
  task: TaskInfo
  form: FormState
  valueDate: string
  /** решение пользователя по предложенной формулировке пункта: findingId -> вставить/отклонить */
  suggestionDecisions: Record<string, 'inserted' | 'rejected'>
  log: LogEntry[]
  aiMode: AiMode
  modelId: string
  modelState: ModelState
  modelProgress: { pct: number; text: string }
  statusStage: StatusStage
  statusHistory: StatusEvent[]
  signedAt: string | null
  /** не сохраняется: текущая подсветка источника в просмотрщике */
  highlight: SourceRef | null
  busy: boolean
  error: string | null

  caseInput: () => CaseInput
  recompute: (rebuildForm?: boolean) => void
  loadKit: (kitId: string) => Promise<void>
  addFiles: (files: File[]) => Promise<void>
  removeDoc: (docId: string) => void
  setTask: (t: Partial<TaskInfo>) => void
  setField: (id: FieldId, value: string) => void
  confirmField: (id: FieldId) => void
  rejectField: (id: FieldId) => void
  setValueDate: (d: string) => void
  setPayments: (p: Payment[]) => void
  submitRegistration: () => void
  setAccountingNumber: (n: string | null) => void
  decideSuggestion: (findingId: string, d: 'inserted' | 'rejected') => void
  logAction: (e: Omit<LogEntry, 'at'>) => void
  setHighlight: (s: SourceRef | null) => void
  setAiMode: (m: AiMode) => void
  setModel: (p: Partial<Pick<AppState, 'modelId' | 'modelState' | 'modelProgress'>>) => void
  sign: () => void
  setStatusStage: (s: StatusStage) => void
  reset: () => void
}

const initial = () => ({
  kitId: null, mock: false, docs: [], extractions: [], findings: [], payments: [],
  accountingNumber: null, registrationSubmittedAt: null,
  task: { text: '', answers: {}, createdAt: null } as TaskInfo,
  form: emptyForm(), valueDate: nextBusinessDay(todayIso()), suggestionDecisions: {}, log: [] as LogEntry[],
  aiMode: 'fallback' as AiMode, modelId: 'Qwen2.5-3B-Instruct-q4f16_1-MLC', modelState: 'idle' as ModelState,
  modelProgress: { pct: 0, text: '' }, statusStage: 'draft' as StatusStage, statusHistory: [] as StatusEvent[],
  signedAt: null, highlight: null, busy: false, error: null,
})

const now = () => new Date().toISOString()

export const useApp = create<AppState>()(
  persist(
    (set, get) => ({
      ...initial(),

      caseInput: () => {
        const s = get()
        return {
          docs: s.docs, extractions: s.extractions, payments: s.payments, accountingNumber: s.accountingNumber,
          valueDate: s.valueDate, form: s.form, accounts: ACCOUNTS, rates: RATES,
        }
      },

      recompute: (rebuildForm = false) => {
        const s = get()
        if (rebuildForm) {
          const form = buildForm(s.caseInput(), s.form)
          set({ form })
        }
        let findings = runChecks(get().caseInput())
        if (s.mock && findings.length === 0) findings = s.findings
        set({ findings })
      },

      loadKit: async (kitId) => {
        set({ busy: true, error: null })
        try {
          const kits: KitManifest[] = await fetch(SAMPLES_BASE + 'kits.json').then((r) => {
            if (!r.ok) throw new Error('kits.json не найден')
            return r.json()
          })
          const kit = kits.find((k) => k.id === kitId)
          if (!kit) throw new Error(`Комплект ${kitId} не найден`)
          const docs: ParsedDoc[] = []
          for (const f of kit.files) {
            const buf = await fetch(SAMPLES_BASE + f).then((r) => r.arrayBuffer())
            docs.push(await parseFile(buf, f.split('/').pop()!, f))
          }
          const extractions = docs.map(extractFields)
          set({
            ...initial(), aiMode: get().aiMode, modelId: get().modelId, modelState: get().modelState,
            task: get().task, kitId, docs, extractions, payments: kit.payments, accountingNumber: kit.accountingNumber,
          })
          get().logAction({ actor: 'user', action: 'uploaded', target: 'Документы', detail: `Загружен демо-комплект ${kit.id}: ${kit.files.length} файла` })
          get().recompute(true)
        } catch (e) {
          const { mockCase } = await import('./dev/mockCase')
          const m = mockCase()
          set({ ...initial(), task: get().task, kitId: 'mock', mock: true, docs: m.docs, extractions: m.extractions, findings: m.findings, form: m.form, payments: m.payments })
          set({ error: `Демо-комплекты ещё не собраны, показаны тестовые данные (${(e as Error).message})` })
        } finally {
          set({ busy: false })
        }
      },

      addFiles: async (files) => {
        set({ busy: true, error: null })
        try {
          const docs = [...get().docs]
          for (const file of files) {
            const doc = await parseFile(await file.arrayBuffer(), file.name, `upload/${file.name}`)
            const i = docs.findIndex((d) => d.id === doc.id)
            if (i >= 0) docs[i] = doc
            else docs.push(doc)
            get().logAction({ actor: 'user', action: 'uploaded', target: 'Документы', detail: `Загружен файл ${file.name}` })
          }
          set({ docs, extractions: docs.map(extractFields), kitId: get().kitId ?? 'upload' })
          get().recompute(true)
        } catch (e) {
          set({ error: `Не удалось прочитать файл: ${(e as Error).message}` })
        } finally {
          set({ busy: false })
        }
      },

      removeDoc: (docId) => {
        const docs = get().docs.filter((d) => d.id !== docId)
        set({ docs, extractions: get().extractions.filter((e) => e.docId !== docId) })
        get().recompute(true)
      },

      setTask: (t) => set({ task: { ...get().task, ...t } }),

      setField: (id, value) => {
        const prev = get().form[id]
        const form = { ...get().form, [id]: { ...prev, value, status: 'user' as const, confirmed: true } }
        set({ form })
        get().logAction({
          actor: 'user', action: prev.suggested !== undefined && prev.suggested !== value ? 'edited' : 'info',
          target: FIELD_LABEL[id], detail: prev.suggested !== undefined ? `Помощник предлагал «${prev.suggested}», введено «${value}»` : `Введено «${value}»`,
        })
        get().recompute(false)
      },

      confirmField: (id) => {
        const prev = get().form[id]
        set({ form: { ...get().form, [id]: { ...prev, confirmed: true } } })
        get().logAction({ actor: 'user', action: 'accepted', target: FIELD_LABEL[id], detail: `Подтверждено значение «${prev.value}»` })
        get().recompute(false)
      },

      rejectField: (id) => {
        const prev = get().form[id]
        set({ form: { ...get().form, [id]: { ...prev, value: '', status: 'empty', confirmed: false, source: undefined } } })
        get().logAction({ actor: 'user', action: 'rejected', target: FIELD_LABEL[id], detail: `Отклонено предложение «${prev.value}»` })
        get().recompute(false)
      },

      setValueDate: (d) => { set({ valueDate: d }); get().recompute(false) },
      setPayments: (p) => { set({ payments: p }); get().recompute(true) },

      submitRegistration: () => {
        set({ registrationSubmittedAt: now() })
        get().logAction({ actor: 'user', action: 'info', target: 'Учётный номер', detail: 'Заявление на учётный номер отправлено (имитация)' })
      },
      setAccountingNumber: (n) => {
        set({ accountingNumber: n })
        if (n) get().logAction({ actor: 'code', action: 'info', target: 'Учётный номер', detail: `Присвоен учётный номер ${n} (имитация)` })
        get().recompute(true)
      },

      decideSuggestion: (findingId, d) => {
        set({ suggestionDecisions: { ...get().suggestionDecisions, [findingId]: d } })
        const f = get().findings.find((x) => x.id === findingId)
        get().logAction({ actor: 'user', action: d === 'inserted' ? 'accepted' : 'rejected', target: f?.title ?? findingId, detail: d === 'inserted' ? 'Формулировка добавлена в протокол разногласий' : 'Предложенная формулировка отклонена' })
      },

      logAction: (e) => set({ log: [...get().log, { ...e, at: now() }] }),
      setHighlight: (s) => set({ highlight: s }),
      setAiMode: (m) => set({ aiMode: m }),
      setModel: (p) => set(p),

      sign: () => {
        const at = now()
        set({ signedAt: at, statusStage: 'signed', statusHistory: [...get().statusHistory, { stage: 'signed', at }] })
        get().logAction({ actor: 'user', action: 'signed', target: 'Заявка на перевод', detail: 'Заявка подписана пользователем (имитация подписи)' })
      },
      setStatusStage: (s) => set({ statusStage: s, statusHistory: [...get().statusHistory, { stage: s, at: now() }] }),

      reset: () => set({ ...initial() }),
    }),
    {
      name: 'ved-assistant-v1',
      storage: createJSONStorage(() => localStorage),
      partialize: (s) => {
        const { highlight, busy, error, modelState, modelProgress, ...rest } = s
        void highlight; void busy; void error; void modelState; void modelProgress
        return rest
      },
    },
  ),
)

/** Нужен ли учётный номер: сумма договора в эквиваленте > порога. */
export function contractTotalUsd(s: Pick<AppState, 'extractions'>): number | null {
  const c = s.extractions.find((e) => e.kind === 'contract')
  const total = c?.fields.totalAmount?.norm
  const cur = (c?.fields.currency?.norm as string) || 'USD'
  if (typeof total !== 'number') return null
  return (total * (RATES[cur] ?? RATES.USD)) / RATES.USD
}

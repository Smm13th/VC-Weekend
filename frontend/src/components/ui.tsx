// Базовые элементы интерфейса. Экраны собираются из них, чтобы вид был единым.
import { Fragment, type ButtonHTMLAttributes, type ReactNode } from 'react'
import type { FieldStatus, Severity } from '../types'

export function cx(...a: (string | false | null | undefined)[]) { return a.filter(Boolean).join(' ') }

type BtnVariant = 'primary' | 'secondary' | 'ghost' | 'danger'
export function Button({ variant = 'secondary', size = 'md', className, ...p }: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: BtnVariant; size?: 'sm' | 'md' }) {
  const v = {
    primary: 'bg-accent text-white border-accent hover:bg-accent-hover disabled:bg-line-strong disabled:border-line-strong',
    secondary: 'bg-surface text-ink border-line-strong hover:bg-sunken disabled:text-ink-3',
    ghost: 'bg-transparent text-accent border-transparent hover:bg-accent-soft disabled:text-ink-3',
    danger: 'bg-surface text-bad border-bad/40 hover:bg-bad-soft',
  }[variant]
  const s = size === 'sm' ? 'h-7 px-2.5 text-[13px]' : 'h-9 px-3.5 text-sm'
  return <button {...p} className={cx('inline-flex items-center justify-center gap-1.5 rounded-[5px] border font-medium transition-colors disabled:cursor-not-allowed', v, s, className)} />
}

export function Panel({ title, actions, children, className, pad = true }: { title?: ReactNode; actions?: ReactNode; children: ReactNode; className?: string; pad?: boolean }) {
  return (
    <section className={cx('rounded-md border border-line bg-surface', className)}>
      {(title || actions) && (
        <header className="flex items-center justify-between gap-3 border-b border-line px-4 py-2.5">
          <h2 className="text-[15px] font-semibold text-ink">{title}</h2>
          {actions && <div className="flex items-center gap-2">{actions}</div>}
        </header>
      )}
      <div className={pad ? 'p-4' : ''}>{children}</div>
    </section>
  )
}

export function PageTitle({ title, lead, right }: { title: string; lead?: ReactNode; right?: ReactNode }) {
  return (
    <div className="mb-4 flex items-start justify-between gap-6">
      <div>
        <h1 className="text-[22px] font-semibold leading-tight text-ink">{title}</h1>
        {lead && <p className="mt-1 max-w-3xl text-ink-2">{lead}</p>}
      </div>
      {right}
    </div>
  )
}

const sevMap: Record<Severity, { label: string; cls: string; dot: string }> = {
  red: { label: 'Блокирует', cls: 'bg-bad-soft text-bad', dot: 'bg-bad' },
  yellow: { label: 'Обсудить с поставщиком', cls: 'bg-warn-soft text-warn', dot: 'bg-warn' },
  green: { label: 'В порядке', cls: 'bg-ok-soft text-ok', dot: 'bg-ok' },
}
export function SeverityBadge({ severity, label }: { severity: Severity; label?: string }) {
  const m = sevMap[severity]
  return <span className={cx('inline-flex items-center gap-1.5 whitespace-nowrap rounded-[4px] px-1.5 py-0.5 text-[12px] font-medium', m.cls)}><span className={cx('h-2 w-2 rounded-[2px]', m.dot)} />{label ?? m.label}</span>
}

const srcMap: Record<FieldStatus, { label: string; cls: string }> = {
  doc: { label: 'Из документа', cls: 'border-accent/30 bg-accent-soft text-accent' },
  ai: { label: 'Догадка помощника, подтвердите', cls: 'border-warn/30 bg-warn-soft text-warn' },
  conflict: { label: 'Не найдено или конфликт', cls: 'border-bad/30 bg-bad-soft text-bad' },
  user: { label: 'Введено вами', cls: 'border-line-strong bg-sunken text-ink-2' },
  system: { label: 'Заполняет система', cls: 'border-line bg-surface text-ink-3' },
  empty: { label: 'Не заполнено', cls: 'border-line bg-surface text-ink-3' },
}
export function SourceBadge({ status, onClick }: { status: FieldStatus; onClick?: () => void }) {
  const m = srcMap[status]
  const C = onClick ? 'button' : 'span'
  return <C type={onClick ? 'button' : undefined} onClick={onClick} className={cx('inline-flex items-center whitespace-nowrap rounded-[4px] border px-1.5 py-px text-[11.5px] font-medium', m.cls, onClick && 'cursor-pointer underline decoration-dotted underline-offset-2')}>{m.label}</C>
}
export const SOURCE_LABEL = Object.fromEntries(Object.entries(srcMap).map(([k, v]) => [k, v.label])) as Record<FieldStatus, string>

type Tone = 'info' | 'warn' | 'bad' | 'ok'
export function Notice({ tone = 'info', title, children, className }: { tone?: Tone; title?: ReactNode; children?: ReactNode; className?: string }) {
  const t = { info: 'border-l-accent bg-accent-soft/60', warn: 'border-l-warn bg-warn-soft', bad: 'border-l-bad bg-bad-soft', ok: 'border-l-ok bg-ok-soft' }[tone]
  return (
    <div className={cx('rounded-[4px] border border-line border-l-[3px] px-3 py-2 text-[13.5px]', t, className)}>
      {title && <div className="font-semibold text-ink">{title}</div>}
      {children && <div className="text-ink-2">{children}</div>}
    </div>
  )
}

/** Обязательная плашка на экранах с проверкой договора и подсказками. */
export function NotLegalAdvice() {
  return <Notice tone="warn">Это подсказка помощника, а не юридическое заключение. Решение по договору принимаете вы вместе с поставщиком и юристом.</Notice>
}

export function Mono({ children, className }: { children: ReactNode; className?: string }) {
  return <span className={cx('mono', className)}>{children}</span>
}

export function KeyValue({ rows }: { rows: [ReactNode, ReactNode][] }) {
  return (
    <dl className="grid grid-cols-[minmax(140px,max-content)_1fr] gap-x-4 gap-y-1.5 text-[13.5px]">
      {rows.map(([k, v], i) => (<Fragment key={i}><dt className="text-ink-3">{k}</dt><dd className="text-ink">{v}</dd></Fragment>))}
    </dl>
  )
}

export function Progress({ value, max }: { value: number; max: number }) {
  const pct = max ? Math.round((value / max) * 100) : 0
  return <div className="h-1.5 w-full overflow-hidden rounded-full bg-sunken"><div className="h-full bg-accent transition-all" style={{ width: pct + '%' }} /></div>
}

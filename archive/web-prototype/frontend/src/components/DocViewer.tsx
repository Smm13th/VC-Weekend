// Просмотр документа с подсветкой источника. Двуязычный договор показывается в две колонки (EN | RU).
import { useEffect, useMemo, useRef } from 'react'
import type { DocBlock, ParsedDoc, SourceRef } from '../types'
import { cx } from './ui'

interface Props {
  doc: ParsedDoc
  /** активная подсветка (прокручивается в видимую область) */
  highlight?: SourceRef | null
  /** дополнительные пометки, например все находки */
  marks?: SourceRef[]
  onMarkClick?: (m: SourceRef) => void
  className?: string
}

function renderBlock(doc: ParsedDoc, b: DocBlock, ranges: { r: SourceRef; active: boolean }[], onMarkClick?: (m: SourceRef) => void, activeRef?: React.RefObject<HTMLElement | null>) {
  const inBlock = ranges
    .filter(({ r }) => r.docId === doc.id && r.start < b.end && r.end > b.start)
    .map((x) => ({ ...x, s: Math.max(x.r.start, b.start), e: Math.min(x.r.end, b.end) }))
    .sort((a, z) => a.s - z.s)
  const out: React.ReactNode[] = []
  let pos = b.start
  inBlock.forEach((x, i) => {
    if (x.s < pos) return
    if (x.s > pos) out.push(doc.text.slice(pos, x.s))
    out.push(
      <mark key={i} ref={x.active ? (activeRef as React.RefObject<HTMLElement>) : undefined} onClick={onMarkClick ? () => onMarkClick(x.r) : undefined}
        className={cx('src-mark', x.active && 'active', onMarkClick && 'cursor-pointer')}>{doc.text.slice(x.s, x.e)}</mark>,
    )
    pos = x.e
  })
  if (pos < b.end) out.push(doc.text.slice(pos, b.end))
  return out
}

export function DocViewer({ doc, highlight, marks = [], onMarkClick, className }: Props) {
  const activeRef = useRef<HTMLElement | null>(null)
  const ranges = useMemo(() => {
    const r = marks.map((m) => ({ r: m, active: false }))
    if (highlight && highlight.docId === doc.id) r.push({ r: highlight, active: true })
    return r
  }, [marks, highlight, doc.id])

  useEffect(() => {
    activeRef.current?.scrollIntoView({ block: 'center', behavior: 'smooth' })
  }, [highlight])

  const rows = useMemo(() => {
    if (!doc.blocks.some((b) => b.row !== undefined)) return null
    const m = new Map<number, { en?: DocBlock; ru?: DocBlock; other: DocBlock[] }>()
    for (const b of doc.blocks) {
      const k = b.row ?? -1
      if (!m.has(k)) m.set(k, { other: [] })
      const e = m.get(k)!
      if (b.lang === 'en' && !e.en) e.en = b
      else if (b.lang === 'ru' && !e.ru) e.ru = b
      else e.other.push(b)
    }
    return [...m.entries()].sort((a, z) => a[0] - z[0])
  }, [doc])

  return (
    <div className={cx('text-[13px] leading-relaxed text-ink', className)}>
      {rows ? (
        <table className="w-full table-fixed border-collapse">
          <thead>
            <tr className="text-left text-[12px] text-ink-3">
              <th className="w-1/2 border-b border-line px-3 py-1.5 font-medium">Английский текст</th>
              <th className="w-1/2 border-b border-l border-line px-3 py-1.5 font-medium">Русский текст</th>
            </tr>
          </thead>
          <tbody>
            {rows.map(([k, r]) => (
              <tr key={k} className="align-top">
                <td className="whitespace-pre-wrap border-b border-line px-3 py-2">{r.en && renderBlock(doc, r.en, ranges, onMarkClick, activeRef)}{r.other.map((b, i) => <div key={i}>{renderBlock(doc, b, ranges, onMarkClick, activeRef)}</div>)}</td>
                <td className="whitespace-pre-wrap border-b border-l border-line px-3 py-2">{r.ru && renderBlock(doc, r.ru, ranges, onMarkClick, activeRef)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <div className="space-y-1 whitespace-pre-wrap px-4 py-3">
          {doc.blocks.map((b, i) => <div key={i}>{renderBlock(doc, b, ranges, onMarkClick, activeRef) }{b.start === b.end && ' '}</div>)}
        </div>
      )}
    </div>
  )
}

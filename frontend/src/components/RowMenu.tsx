import { useEffect, useRef, useState } from 'react'
import { FaEllipsisVertical } from 'react-icons/fa6'

export interface RowMenuItem {
  label: string
  danger?: boolean
  action: () => void
}

export function RowMenu({ items }: { items: RowMenuItem[] }) {
  const [open, setOpen] = useState(false)
  const [confirming, setConfirming] = useState<string | null>(null)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false)
        setConfirming(null)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  return (
    <div className='row-menu' ref={ref} onClick={(e) => e.stopPropagation()}>
      <button
        type='button'
        className='row-menu__trigger'
        onClick={(e) => { e.stopPropagation(); setOpen((v) => !v); setConfirming(null) }}
      >
        <FaEllipsisVertical />
      </button>
      {open && (
        <div className='row-menu__dropdown'>
          {items.map((item) => (
            confirming === item.label
              ? (
                <div key={item.label} className='row-menu__confirm'>
                  <span>Sure?</span>
                  <button type='button' onClick={() => { item.action(); setOpen(false); setConfirming(null) }}>Yes</button>
                  <button type='button' onClick={() => setConfirming(null)}>No</button>
                </div>
              )
              : (
                <button
                  key={item.label}
                  type='button'
                  className={`row-menu__item${item.danger ? ' row-menu__item--danger' : ''}`}
                  onClick={() => item.danger ? setConfirming(item.label) : (item.action(), setOpen(false))}
                >
                  {item.label}
                </button>
              )
          ))}
        </div>
      )}
    </div>
  )
}

export default RowMenu

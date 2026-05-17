import { describe, it, expect, vi } from 'vitest'
import { renderHook } from '@testing-library/react'
import { useEscapeKey } from './useEscapeKey'

function fireEscape() {
  window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
}

function fireOther() {
  window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
}

describe('useEscapeKey', () => {
  it('calls onEscape when Escape key is pressed', () => {
    const onEscape = vi.fn()
    renderHook(() => useEscapeKey(onEscape))
    fireEscape()
    expect(onEscape).toHaveBeenCalledTimes(1)
  })

  it('does not call onEscape for other keys', () => {
    const onEscape = vi.fn()
    renderHook(() => useEscapeKey(onEscape))
    fireOther()
    expect(onEscape).not.toHaveBeenCalled()
  })

  it('does not call onEscape when disabled', () => {
    const onEscape = vi.fn()
    renderHook(() => useEscapeKey(onEscape, false))
    fireEscape()
    expect(onEscape).not.toHaveBeenCalled()
  })

  it('removes listener on unmount', () => {
    const onEscape = vi.fn()
    const { unmount } = renderHook(() => useEscapeKey(onEscape))
    unmount()
    fireEscape()
    expect(onEscape).not.toHaveBeenCalled()
  })

  it('re-enables when enabled flips from false to true', () => {
    const onEscape = vi.fn()
    const { rerender } = renderHook(({ enabled }) => useEscapeKey(onEscape, enabled), {
      initialProps: { enabled: false },
    })
    fireEscape()
    expect(onEscape).not.toHaveBeenCalled()

    rerender({ enabled: true })
    fireEscape()
    expect(onEscape).toHaveBeenCalledTimes(1)
  })
})

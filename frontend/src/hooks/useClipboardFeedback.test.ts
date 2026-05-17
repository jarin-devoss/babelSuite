import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useClipboardFeedback } from './useClipboardFeedback'

describe('useClipboardFeedback', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText: vi.fn().mockResolvedValue(undefined) },
      writable: true,
      configurable: true,
    })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('starts with empty copiedId', () => {
    const { result } = renderHook(() => useClipboardFeedback())
    expect(result.current.copiedId).toBe('')
  })

  it('sets copiedId after copyToClipboard', async () => {
    const { result } = renderHook(() => useClipboardFeedback())

    await act(async () => {
      await result.current.copyToClipboard('header-1', 'value')
    })

    expect(result.current.copiedId).toBe('header-1')
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('value')
  })

  it('clears copiedId after timeout', async () => {
    const { result } = renderHook(() => useClipboardFeedback(1000))

    await act(async () => {
      await result.current.copyToClipboard('btn', 'text')
    })

    expect(result.current.copiedId).toBe('btn')

    act(() => { vi.advanceTimersByTime(1000) })

    expect(result.current.copiedId).toBe('')
  })

  it('clearCopied resets copiedId immediately', async () => {
    const { result } = renderHook(() => useClipboardFeedback())

    await act(async () => {
      await result.current.copyToClipboard('x', 'val')
    })

    act(() => { result.current.clearCopied() })

    expect(result.current.copiedId).toBe('')
  })

  it('overwriting a pending copy replaces the id', async () => {
    const { result } = renderHook(() => useClipboardFeedback(2000))

    await act(async () => {
      await result.current.copyToClipboard('first', 'a')
    })
    await act(async () => {
      await result.current.copyToClipboard('second', 'b')
    })

    expect(result.current.copiedId).toBe('second')
  })
})

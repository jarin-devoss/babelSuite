import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest'
import { act, fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ExecutionDag } from './ExecutionDag'
import { groupTopologyByLevel, type RuntimeStatus, type TopologyNode } from '../../lib/suites'

// Callbacks of every live ResizeObserver, so a test can give the scroll
// container a size. jsdom reports 0x0 for everything and never fires the
// observer, which would otherwise leave the component permanently mid-measure.
let resizeCallbacks: Array<() => void> = []

beforeAll(() => {
  globalThis.ResizeObserver = class {
    constructor(callback: () => void) { resizeCallbacks.push(callback) }
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver

  // jsdom has no PointerEvent, and without it fireEvent falls back to a plain
  // Event that carries neither `button` nor `pointerId` — so a drag would
  // never start and the test would pass for the wrong reason. MouseEvent
  // supplies button and clientX/Y; pointerId is the only addition.
  if (!globalThis.PointerEvent) {
    class PointerEventPolyfill extends MouseEvent {
      readonly pointerId: number
      constructor(type: string, params: MouseEventInit & { pointerId?: number } = {}) {
        super(type, params)
        this.pointerId = params.pointerId ?? 1
      }
    }
    globalThis.PointerEvent = PointerEventPolyfill as unknown as typeof PointerEvent
  }

  // Pointer capture keeps a drag alive past the edge of the element.
  Element.prototype.setPointerCapture ??= function () {}
  Element.prototype.hasPointerCapture ??= function () { return false }
  Element.prototype.releasePointerCapture ??= function () {}
})

beforeEach(() => {
  resizeCallbacks = []
})

function node(id: string, level: number, dependsOn: string[] = []): TopologyNode {
  return { id, name: id, kind: 'task', dependsOn, level }
}

/** A chain wide enough that it cannot fit, which is what enables panning. */
function wideTopology(levels = 14): TopologyNode[] {
  return Array.from({ length: levels }, (_, i) =>
    node(`step-${i}`, i, i === 0 ? [] : [`step-${i - 1}`]),
  )
}

function renderDag(nodes: TopologyNode[], onSelectSource = vi.fn()) {
  const statusMap: Record<string, RuntimeStatus> = {}
  nodes.forEach((n) => { statusMap[n.id] = 'healthy' })
  const result = render(
    <ExecutionDag
      topology={groupTopologyByLevel(nodes)}
      flatTopology={nodes}
      statusMap={statusMap}
      selectedSource=''
      onSelectSource={onSelectSource}
      onClose={vi.fn()}
    />,
  )
  const scroll = document.querySelector('.dag-scroll') as HTMLElement
  return { ...result, scroll, onSelectSource }
}

/**
 * Give the scroll container a viewport and a working scroll offset, then let
 * the component re-measure. jsdom reports every element as 0x0 and does not
 * implement scrolling, so both have to be supplied.
 */
function setViewport(scroll: HTMLElement, clientW: number, clientH: number) {
  Object.defineProperty(scroll, 'clientWidth', { value: clientW, configurable: true })
  Object.defineProperty(scroll, 'clientHeight', { value: clientH, configurable: true })
  let left = 0
  let top = 0
  Object.defineProperty(scroll, 'scrollLeft', {
    get: () => left, set: (v: number) => { left = v }, configurable: true,
  })
  Object.defineProperty(scroll, 'scrollTop', {
    get: () => top, set: (v: number) => { top = v }, configurable: true,
  })
  act(() => { resizeCallbacks.forEach((cb) => cb()) })
}

/** Zoom in n steps. Panning only applies once the graph exceeds the viewport. */
function zoomIn(steps: number) {
  const button = screen.getByLabelText('Zoom in')
  for (let i = 0; i < steps; i += 1) fireEvent.click(button)
}

/** The zoom readout, which doubles as the reset-to-fit button. */
const zoomLevel = () => document.querySelector('.dag-zoom__level') as HTMLElement
const zoomPercent = () => Number(zoomLevel().textContent!.replace('%', ''))

describe('ExecutionDag', () => {
  it('renders a node per topology entry', () => {
    renderDag([node('db', 0), node('migrate', 1, ['db'])])
    expect(screen.getByText('db')).toBeInTheDocument()
    expect(screen.getByText('migrate')).toBeInTheDocument()
  })

  it('selects a node on click', async () => {
    const user = userEvent.setup()
    const { onSelectSource } = renderDag([node('db', 0)])
    await user.click(screen.getByText('db'))
    expect(onSelectSource).toHaveBeenCalledWith('db')
  })

  describe('zoom', () => {
    it('zooms in past 100%, so a large graph can be magnified rather than only shrunk', async () => {
      const user = userEvent.setup()
      renderDag(wideTopology())
      await user.click(screen.getByLabelText('Zoom in'))
      expect(zoomPercent()).toBeGreaterThan(100)
    })

    it('returns to the fit scale when the level is clicked', async () => {
      const user = userEvent.setup()
      renderDag(wideTopology())
      await user.click(screen.getByLabelText('Zoom in'))
      const zoomed = zoomPercent()
      await user.click(zoomLevel())
      expect(zoomPercent()).not.toEqual(zoomed)
    })

    it('stops zooming out at the floor', async () => {
      const user = userEvent.setup()
      renderDag(wideTopology())
      const out = screen.getByLabelText('Zoom out')
      for (let i = 0; i < 40; i += 1) {
        if ((out as HTMLButtonElement).disabled) break
        await user.click(out)
      }
      expect(out).toBeDisabled()
      expect(zoomPercent()).toEqual(10)
    })
  })

  describe('drag to pan', () => {
    it('scrolls the canvas by the drag distance', () => {
      const { scroll } = renderDag(wideTopology())
      setViewport(scroll, 400, 300)
      zoomIn(3)   // at the fit scale the graph fits, so there is nothing to pan to
      scroll.scrollLeft = 200
      scroll.scrollTop = 100

      fireEvent.pointerDown(scroll, { button: 0, pointerId: 1, clientX: 300, clientY: 200 })
      fireEvent.pointerMove(scroll, { pointerId: 1, clientX: 240, clientY: 170 })
      fireEvent.pointerUp(scroll, { pointerId: 1, clientX: 240, clientY: 170 })

      // Dragging left moves the viewport right, so the offset grows.
      expect(scroll.scrollLeft).toBe(260)
      expect(scroll.scrollTop).toBe(130)
    })

    it('ignores movement below the click threshold, so a node is still selectable', () => {
      const { scroll } = renderDag(wideTopology())
      setViewport(scroll, 400, 300)
      zoomIn(3)
      scroll.scrollLeft = 200

      fireEvent.pointerDown(scroll, { button: 0, pointerId: 1, clientX: 300, clientY: 200 })
      // 2px — inside PAN_THRESHOLD, the jitter of an ordinary click.
      fireEvent.pointerMove(scroll, { pointerId: 1, clientX: 302, clientY: 200 })
      fireEvent.pointerUp(scroll, { pointerId: 1, clientX: 302, clientY: 200 })

      expect(scroll.scrollLeft).toBe(200)
    })

    it('does not select the node a drag happens to finish on', () => {
      const { scroll, onSelectSource } = renderDag(wideTopology())
      setViewport(scroll, 400, 300)
      zoomIn(3)
      const target = screen.getByText('step-0')

      fireEvent.pointerDown(scroll, { button: 0, pointerId: 1, clientX: 300, clientY: 200 })
      fireEvent.pointerMove(scroll, { pointerId: 1, clientX: 180, clientY: 200 })
      fireEvent.pointerUp(scroll, { pointerId: 1, clientX: 180, clientY: 200 })
      fireEvent.click(target)

      expect(onSelectSource).not.toHaveBeenCalled()
    })

    it('still selects on the click after a drag has finished', () => {
      const { scroll, onSelectSource } = renderDag(wideTopology())
      setViewport(scroll, 400, 300)
      zoomIn(3)

      fireEvent.pointerDown(scroll, { button: 0, pointerId: 1, clientX: 300, clientY: 200 })
      fireEvent.pointerMove(scroll, { pointerId: 1, clientX: 180, clientY: 200 })
      fireEvent.pointerUp(scroll, { pointerId: 1, clientX: 180, clientY: 200 })
      fireEvent.click(scroll)

      // The next press resets the drag flag, so this click is honoured.
      fireEvent.pointerDown(scroll, { button: 0, pointerId: 2, clientX: 300, clientY: 200 })
      fireEvent.pointerUp(scroll, { pointerId: 2, clientX: 300, clientY: 200 })
      fireEvent.click(screen.getByText('step-0'))

      expect(onSelectSource).toHaveBeenCalledWith('step-0')
    })

    it('ignores a non-primary button so the context menu is unaffected', () => {
      const { scroll } = renderDag(wideTopology())
      setViewport(scroll, 400, 300)
      zoomIn(3)
      scroll.scrollLeft = 200

      fireEvent.pointerDown(scroll, { button: 2, pointerId: 1, clientX: 300, clientY: 200 })
      fireEvent.pointerMove(scroll, { pointerId: 1, clientX: 180, clientY: 200 })

      expect(scroll.scrollLeft).toBe(200)
    })

    it('advertises panning only once something is off-screen', () => {
      const wide = renderDag(wideTopology())
      setViewport(wide.scroll, 400, 300)
      // At the fit scale the whole graph is visible, so a grab cursor would be
      // a lie — there is nowhere to drag to.
      expect(wide.scroll.className).not.toContain('dag-scroll--pannable')

      zoomIn(3)
      expect(wide.scroll.className).toContain('dag-scroll--pannable')
      wide.unmount()

      const small = renderDag([node('db', 0), node('migrate', 1, ['db'])])
      setViewport(small.scroll, 4000, 3000)
      expect(small.scroll.className).not.toContain('dag-scroll--pannable')
    })
  })

  describe('keyboard', () => {
    it('pans with the arrow keys', () => {
      const { scroll } = renderDag(wideTopology())
      setViewport(scroll, 400, 300)
      scroll.scrollLeft = 200

      fireEvent.keyDown(window, { key: 'ArrowRight' })
      expect(scroll.scrollLeft).toBe(280)
      fireEvent.keyDown(window, { key: 'ArrowLeft' })
      expect(scroll.scrollLeft).toBe(200)
    })

    it('zooms with + and - and resets with 0', () => {
      renderDag(wideTopology())
      const fit = zoomPercent()

      fireEvent.keyDown(window, { key: '+' })
      expect(zoomPercent()).not.toEqual(fit)
      fireEvent.keyDown(window, { key: '0' })
      expect(zoomPercent()).toEqual(fit)
    })
  })
})

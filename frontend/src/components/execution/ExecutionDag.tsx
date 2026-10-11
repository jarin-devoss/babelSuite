import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { FaMagnifyingGlassMinus, FaMagnifyingGlassPlus, FaXmark } from 'react-icons/fa6'
import { useEscapeKey } from '../../hooks/useEscapeKey'
import type { groupTopologyByLevel, RuntimeStatus } from '../../lib/suites'
import './ExecutionDag.css'

type TopologyLevels = ReturnType<typeof groupTopologyByLevel>
type TopologyNode = TopologyLevels[number][number]

const DAG_NODE_W = 180
const DAG_NODE_H = 62
const DAG_COL_GAP = 110
const DAG_ROW_GAP = 20
const DAG_PAD = 40

// A composed suite reaches ~70 nodes, and fitting that into a viewport shrinks
// the labels past reading. Zoom is therefore independent of the fit scale: fit
// is only the starting point, and MAX_SCALE is above 1 so a large graph can be
// magnified rather than merely un-shrunk.
const MIN_SCALE = 0.1
const MAX_SCALE = 3
const ZOOM_STEP = 1.25

// Pixels the pointer may travel before a press counts as a drag rather than a
// click. Without it, the small movement in an ordinary click would pan the
// graph and swallow the node selection.
const PAN_THRESHOLD = 4
// How far the arrow keys scroll, so the graph is navigable without a mouse.
const KEY_PAN_STEP = 80

export function ExecutionDag({
  topology,
  flatTopology,
  statusMap,
  selectedSource,
  onSelectSource,
  onClose,
}: {
  topology: TopologyLevels
  flatTopology: TopologyNode[]
  statusMap: Record<string, RuntimeStatus>
  selectedSource: string
  onSelectSource: (id: string) => void
  onClose: () => void
}) {
  const scrollRef = useRef<HTMLDivElement>(null)
  const closeRef = useRef<HTMLButtonElement>(null)
  const [containerSize, setContainerSize] = useState({ w: 0, h: 0 })

  useEscapeKey(onClose)

  useEffect(() => {
    const prior = document.activeElement as HTMLElement | null
    closeRef.current?.focus()
    return () => { prior?.focus() }
  }, [])

  useEffect(() => {
    const el = scrollRef.current
    if (!el) return
    const update = () => setContainerSize({ w: el.clientWidth, h: el.clientHeight })
    update()
    const ro = new ResizeObserver(update)
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const maxNodes = Math.max(...topology.map((w) => w.length), 1)
  const totalH = maxNodes * (DAG_NODE_H + DAG_ROW_GAP) - DAG_ROW_GAP

  const positions = useMemo(() => {
    const map = new Map<string, { x: number; y: number }>()
    topology.forEach((wave, wi) => {
      const colH = wave.length * (DAG_NODE_H + DAG_ROW_GAP) - DAG_ROW_GAP
      const startY = (totalH - colH) / 2 + DAG_PAD
      wave.forEach((node, ni) => {
        map.set(node.id, {
          x: wi * (DAG_NODE_W + DAG_COL_GAP) + DAG_PAD,
          y: startY + ni * (DAG_NODE_H + DAG_ROW_GAP),
        })
      })
    })
    return map
  }, [topology, totalH])

  const canvasW = topology.length * (DAG_NODE_W + DAG_COL_GAP) - DAG_COL_GAP + DAG_PAD * 2
  const canvasH = totalH + DAG_PAD * 2

  // The scale that shows the whole graph. Capped at 1 so a small topology is
  // not blown up on open.
  const fitScale = containerSize.w > 0 && containerSize.h > 0
    ? Math.min(1, (containerSize.w - DAG_PAD) / canvasW, (containerSize.h - DAG_PAD) / canvasH)
    : 1

  // null means "follow the fit", so the graph keeps re-fitting while the window
  // resizes until the reader zooms, after which their choice is kept.
  const [userScale, setUserScale] = useState<number | null>(null)
  const scale = userScale ?? fitScale
  const scaledW = Math.ceil(canvasW * scale)
  const scaledH = Math.ceil(canvasH * scale)

  // Zooming changes the canvas size, so the scroll offset has to be corrected
  // afterwards or the view jumps. The anchor records the canvas-space point to
  // hold still, and is applied once the new size is laid out.
  const anchorRef = useRef<{ cx: number; cy: number; vx: number; vy: number } | null>(null)

  const zoomTo = useCallback((next: number, viewportX?: number, viewportY?: number) => {
    const el = scrollRef.current
    const clamped = Math.min(MAX_SCALE, Math.max(MIN_SCALE, next))
    if (el) {
      // Default to the viewport centre; a wheel gesture passes the pointer so
      // the graph zooms about whatever is under the cursor.
      const vx = viewportX ?? el.clientWidth / 2
      const vy = viewportY ?? el.clientHeight / 2
      anchorRef.current = {
        cx: (el.scrollLeft + vx) / scale,
        cy: (el.scrollTop + vy) / scale,
        vx,
        vy,
      }
    }
    setUserScale(clamped)
  }, [scale])

  useEffect(() => {
    const el = scrollRef.current
    const anchor = anchorRef.current
    if (!el || !anchor) return
    anchorRef.current = null
    el.scrollLeft = anchor.cx * scale - anchor.vx
    el.scrollTop = anchor.cy * scale - anchor.vy
  }, [scale])

  // Ctrl/Cmd + wheel is the conventional zoom gesture, and has to be a native
  // listener because React's onWheel is passive and cannot preventDefault —
  // without which the browser zooms the whole page instead.
  useEffect(() => {
    const el = scrollRef.current
    if (!el) return
    const onWheel = (event: WheelEvent) => {
      if (!event.ctrlKey && !event.metaKey) return
      event.preventDefault()
      const rect = el.getBoundingClientRect()
      const factor = event.deltaY < 0 ? ZOOM_STEP : 1 / ZOOM_STEP
      zoomTo(scale * factor, event.clientX - rect.left, event.clientY - rect.top)
    }
    el.addEventListener('wheel', onWheel, { passive: false })
    return () => el.removeEventListener('wheel', onWheel)
  }, [scale, zoomTo])

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.ctrlKey || event.metaKey || event.altKey) return
      const el = scrollRef.current
      switch (event.key) {
        case '+': case '=': event.preventDefault(); zoomTo(scale * ZOOM_STEP); break
        case '-': case '_': event.preventDefault(); zoomTo(scale / ZOOM_STEP); break
        case '0': event.preventDefault(); setUserScale(null); break
        // Dragging is mouse-only, so the arrows are how a keyboard reader
        // moves around a graph wider than the screen.
        case 'ArrowLeft': if (el) { event.preventDefault(); el.scrollLeft -= KEY_PAN_STEP } break
        case 'ArrowRight': if (el) { event.preventDefault(); el.scrollLeft += KEY_PAN_STEP } break
        case 'ArrowUp': if (el) { event.preventDefault(); el.scrollTop -= KEY_PAN_STEP } break
        case 'ArrowDown': if (el) { event.preventDefault(); el.scrollTop += KEY_PAN_STEP } break
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [scale, zoomTo])

  // Drag to pan. A press anywhere on the canvas starts it, including on a node,
  // because a dense graph leaves little background to grab — so a click is
  // distinguished from a drag by distance travelled rather than by target.
  const panRef = useRef<{ x: number; y: number; left: number; top: number; id: number } | null>(null)
  const didPanRef = useRef(false)
  const [isPanning, setIsPanning] = useState(false)

  const canPan = scaledW > containerSize.w || scaledH > containerSize.h

  const onPointerDown = (event: React.PointerEvent<HTMLDivElement>) => {
    if (event.button !== 0 || !canPan) return
    const el = scrollRef.current
    if (!el) return
    panRef.current = { x: event.clientX, y: event.clientY, left: el.scrollLeft, top: el.scrollTop, id: event.pointerId }
    didPanRef.current = false
  }

  const onPointerMove = (event: React.PointerEvent<HTMLDivElement>) => {
    const pan = panRef.current
    const el = scrollRef.current
    if (!pan || !el || event.pointerId !== pan.id) return

    const dx = event.clientX - pan.x
    const dy = event.clientY - pan.y
    if (!didPanRef.current) {
      if (Math.hypot(dx, dy) < PAN_THRESHOLD) return
      didPanRef.current = true
      setIsPanning(true)
      // Capture so the drag keeps working past the edge of the viewport and
      // over a node, which would otherwise take the pointer events.
      el.setPointerCapture?.(event.pointerId)
    }
    el.scrollLeft = pan.left - dx
    el.scrollTop = pan.top - dy
  }

  const endPan = (event: React.PointerEvent<HTMLDivElement>) => {
    const el = scrollRef.current
    if (el?.hasPointerCapture?.(event.pointerId)) el.releasePointerCapture(event.pointerId)
    panRef.current = null
    setIsPanning(false)
  }

  // A drag that finished over a node would otherwise fire that node's click and
  // re-filter the logs. didPanRef is reset on the next press, so this only ever
  // swallows the click belonging to the drag that just ended.
  const onClickCapture = (event: React.MouseEvent<HTMLDivElement>) => {
    if (!didPanRef.current) return
    event.preventDefault()
    event.stopPropagation()
  }

  const edges = useMemo(() => {
    const result: Array<{ fromId: string; toId: string; status: RuntimeStatus }> = []
    flatTopology.forEach((node) => {
      const toPos = positions.get(node.id)
      if (!toPos) return
      node.dependsOn.forEach((depId) => {
        const fromPos = positions.get(depId)
        if (!fromPos) return
        result.push({ fromId: depId, toId: node.id, status: statusMap[depId] })
      })
    })
    return result
  }, [flatTopology, positions, statusMap])

  return createPortal(
    <div className='dag-overlay' role='dialog' aria-modal='true' aria-label='Topology Graph'>
      <div className='dag-header'>
        <span className='dag-header__title'>Topology Graph</span>
        <div className='dag-header__hint'>
          {canPan
            ? 'Drag to move · Ctrl/⌘ + scroll to zoom · arrows to pan · click a node to filter logs'
            : 'Click a node to filter logs · Ctrl/⌘ + scroll to zoom'}
        </div>

        <div className='dag-zoom' role='group' aria-label='Zoom'>
          <button
            type='button'
            className='dag-zoom__btn'
            onClick={() => zoomTo(scale / ZOOM_STEP)}
            disabled={scale <= MIN_SCALE}
            aria-label='Zoom out'
            title='Zoom out  (-)'
          >
            <FaMagnifyingGlassMinus />
          </button>
          <button
            type='button'
            className='dag-zoom__level'
            onClick={() => setUserScale(null)}
            aria-label={`Zoom ${Math.round(scale * 100)} percent — reset to fit`}
            title='Reset to fit  (0)'
          >
            {Math.round(scale * 100)}%
          </button>
          <button
            type='button'
            className='dag-zoom__btn'
            onClick={() => zoomTo(scale * ZOOM_STEP)}
            disabled={scale >= MAX_SCALE}
            aria-label='Zoom in'
            title='Zoom in  (+)'
          >
            <FaMagnifyingGlassPlus />
          </button>
        </div>

        <button ref={closeRef} type='button' className='dag-close' onClick={onClose}>
          <FaXmark />
          <span>Close</span>
        </button>
      </div>

      <div
        className={`dag-scroll${canPan ? ' dag-scroll--pannable' : ''}${isPanning ? ' dag-scroll--panning' : ''}`}
        ref={scrollRef}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={endPan}
        onPointerCancel={endPan}
        onClickCapture={onClickCapture}
      >
        <div style={{ width: scaledW, height: scaledH, position: 'relative' }}>
          <div className='dag-canvas' style={{ width: canvasW, height: canvasH, transform: `scale(${scale})`, transformOrigin: 'top left' }}>
            <svg className='dag-svg' width={canvasW} height={canvasH}>
              <defs>
                <marker id='dag-arrow-pending' markerWidth='6' markerHeight='6' refX='5' refY='3' orient='auto'>
                  <path d='M0,0 L6,3 L0,6 Z' fill='#1e3a4e' />
                </marker>
                <marker id='dag-arrow-running' markerWidth='6' markerHeight='6' refX='5' refY='3' orient='auto'>
                  <path d='M0,0 L6,3 L0,6 Z' fill='#0DADEA' />
                </marker>
                <marker id='dag-arrow-healthy' markerWidth='6' markerHeight='6' refX='5' refY='3' orient='auto'>
                  <path d='M0,0 L6,3 L0,6 Z' fill='#18BE94' />
                </marker>
                <marker id='dag-arrow-failed' markerWidth='6' markerHeight='6' refX='5' refY='3' orient='auto'>
                  <path d='M0,0 L6,3 L0,6 Z' fill='#E96D76' />
                </marker>
                <marker id='dag-arrow-skipped' markerWidth='6' markerHeight='6' refX='5' refY='3' orient='auto'>
                  <path d='M0,0 L6,3 L0,6 Z' fill='#7c8b98' />
                </marker>
              </defs>

              {edges.map(({ fromId, toId, status }) => {
                const fp = positions.get(fromId)
                const tp = positions.get(toId)
                if (!fp || !tp) return null
                const sx = fp.x + DAG_NODE_W
                const sy = fp.y + DAG_NODE_H / 2
                const tx = tp.x - 6
                const ty = tp.y + DAG_NODE_H / 2
                const cx = sx + (tx - sx) / 2
                const stroke =
                  status === 'healthy' ? '#18BE94'
                  : status === 'running' ? '#0DADEA'
                  : status === 'failed' ? '#E96D76'
                  : status === 'skipped' ? '#7c8b98'
                  : '#1e3a4e'
                return (
                  <path
                    key={`${fromId}-${toId}`}
                    className={`dag-edge${status === 'running' ? ' dag-edge--running' : ''}`}
                    d={`M ${sx} ${sy} C ${cx} ${sy}, ${cx} ${ty}, ${tx} ${ty}`}
                    fill='none'
                    stroke={stroke}
                    strokeWidth={1.5}
                    strokeOpacity={0.55}
                    markerEnd={`url(#dag-arrow-${status})`}
                  />
                )
              })}
            </svg>

            {flatTopology.map((node) => {
              const pos = positions.get(node.id)
              if (!pos) return null
              const status = statusMap[node.id]
              return (
                <button
                  key={node.id}
                  type='button'
                  className={`dag-node dag-node--${status}${selectedSource === node.id ? ' dag-node--selected' : ''}`}
                  style={{ left: pos.x, top: pos.y, width: DAG_NODE_W, height: DAG_NODE_H }}
                  onClick={() => onSelectSource(node.id)}
                >
                  <span className={`exec-dot exec-dot--${status}`} />
                  <div className='dag-node__text'>
                    <strong>{node.name}</strong>
                    <span>{node.kind}</span>
                  </div>
                </button>
              )
            })}
          </div>
        </div>
      </div>
    </div>,
    document.body,
  )
}

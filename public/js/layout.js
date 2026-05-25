// @ts-check

import {
  GAP,
  GROUP_GAP,
  HEADER_HEIGHT,
  MOBILE_BREAKPOINT,
  MOBILE_COLS,
  TARGET_ROW_HEIGHT
} from './gallery-state.js'

export function computeLayout (items, groupByDate, containerW) {
  const tileLayout = new Array(items.length)
  const newHeaders = []
  const groups = groupByDate ? groupItemsByMonth(items) : [{ label: null, indices: itemIndices(items) }]
  const isMobile = containerW < MOBILE_BREAKPOINT
  let y = 0
  for (let g = 0; g < groups.length; g++) {
    const group = groups[g]
    if (group.label) {
      newHeaders.push({ label: group.label, top: y, height: HEADER_HEIGHT })
      y += HEADER_HEIGHT
    }
    y = isMobile
      ? layoutSquareGroup(containerW, group.indices, y, tileLayout)
      : layoutJustifiedGroup(items, containerW, group.indices, y, tileLayout)
    if (g < groups.length - 1) y += GROUP_GAP
  }
  return {
    layout: tileLayout,
    headers: newHeaders,
    totalHeight: Math.max(0, y)
  }
}

function itemIndices (items) {
  const out = new Array(items.length)
  for (let i = 0; i < items.length; i++) out[i] = i
  return out
}

function groupItemsByMonth (items) {
  const map = new Map()
  for (let i = 0; i < items.length; i++) {
    const key = (items[i].fileCreatedAt || '').slice(0, 7) || 'undated'
    let g = map.get(key)
    if (!g) {
      g = { label: monthLabel(key), indices: [] }
      map.set(key, g)
    }
    g.indices.push(i)
  }
  return Array.from(map.values())
}

function monthLabel (key) {
  if (key === 'undated') return 'Undated'
  const parts = key.split('-')
  const y = Number(parts[0])
  const m = Number(parts[1])
  if (!y || !m) return key
  return new Intl.DateTimeFormat(undefined, {
    year: 'numeric',
    month: 'long',
    timeZone: 'UTC'
  }).format(new Date(Date.UTC(y, m - 1, 1)))
}

function layoutSquareGroup (containerW, indices, startY, tileLayout) {
  const tileSize = Math.floor((containerW - (MOBILE_COLS - 1) * GAP) / MOBILE_COLS)
  let col = 0
  let x = 0
  let y = startY
  for (const idx of indices) {
    tileLayout[idx] = { index: idx, left: x, top: y, width: tileSize, height: tileSize }
    col++
    if (col === MOBILE_COLS) {
      col = 0
      x = 0
      y += tileSize + GAP
    } else {
      x += tileSize + GAP
    }
  }
  if (col > 0) y += tileSize
  else if (y > startY) y -= GAP
  return y
}

function layoutJustifiedGroup (items, containerW, indices, startY, tileLayout) {
  let rowItems = []
  let aspectSum = 0
  let y = startY

  const applyRow = (rowEntries, height, isLastRow) => {
    const intHeight = Math.floor(height)
    let x = 0
    rowEntries.forEach(({ idx, aspect }, i) => {
      const isFinalInRow = i === rowEntries.length - 1
      const w = (!isLastRow && isFinalInRow)
        ? containerW - x
        : Math.floor(aspect * height)
      tileLayout[idx] = { index: idx, left: x, top: y, width: w, height: intHeight }
      x += w + GAP
    })
    y += intHeight + GAP
  }

  for (const idx of indices) {
    const item = items[idx]
    const w = item.width || 1
    const h = item.height || 1
    const aspect = w / h
    rowItems.push({ idx, aspect })
    aspectSum += aspect
    const projectedH = (containerW - (rowItems.length - 1) * GAP) / aspectSum
    if (projectedH <= TARGET_ROW_HEIGHT) {
      applyRow(rowItems, projectedH, false)
      rowItems = []
      aspectSum = 0
    }
  }
  if (rowItems.length) applyRow(rowItems, TARGET_ROW_HEIGHT, true)
  return y > startY ? y - GAP : y
}

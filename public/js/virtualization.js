// @ts-check

import {
  BUFFER_VIEWPORTS,
  CHECK_SVG,
  IMAGE_LOAD_MARGIN_PX,
  LONG_PRESS_MS,
  SCROLL_SETTLE_MS
} from '/assets/js/gallery-state.js'
import { decodeThumbhash } from './thumbhash.js'
import { computeLayout } from './layout.js'
import { openLightbox } from './lightbox.js'

export function setupToolbar (state) {
  state.toolbarEl = document.getElementById('select-toolbar')
  if (!state.toolbarEl) return
  state.countEl = document.getElementById('select-count')
  state.selectAllBtn = /** @type {HTMLButtonElement | null} */ (document.getElementById('select-all'))
  state.cancelBtn = /** @type {HTMLButtonElement | null} */ (document.getElementById('select-cancel'))
  state.downloadBtn = /** @type {HTMLButtonElement | null} */ (document.getElementById('select-download'))
  if (state.cancelBtn) state.cancelBtn.addEventListener('click', () => exitSelectMode(state))
  if (state.selectAllBtn) state.selectAllBtn.addEventListener('click', () => selectAllOrNone(state))
  if (state.downloadBtn) state.downloadBtn.addEventListener('click', () => downloadSelected(state))
  updateSelectionUI(state)
}

export function computeLayoutAndRender (state) {
  if (!state.container) return
  const containerW = state.container.clientWidth
  if (containerW <= 0) return
  if (containerW === state.lastContainerW) {
    virtualize(state)
    return
  }
  state.lastContainerW = containerW

  const result = computeLayout(state.items, state.groupByDate, containerW)
  state.layout = result.layout
  state.headers = result.headers
  state.container.style.height = result.totalHeight + 'px'

  for (const [, el] of state.renderedTiles) el.remove()
  state.renderedTiles.clear()
  for (const [, el] of state.renderedHeaders) el.remove()
  state.renderedHeaders.clear()

  virtualize(state)
  loadVisibleTiles(state)
}

export function onScroll (state) {
  if (state.scrollFrame == null) {
    state.scrollFrame = requestAnimationFrame(() => {
      state.scrollFrame = null
      virtualize(state)
    })
  }
  if (state.loadTimer) clearTimeout(state.loadTimer)
  state.loadTimer = setTimeout(() => {
    state.loadTimer = null
    loadVisibleTiles(state)
  }, SCROLL_SETTLE_MS)
}

function onThumbError () {
  this.closest('a').classList.add('thumb-error')
}

function createTile (state, index) {
  const item = state.items[index]
  const l = state.layout[index]
  const a = document.createElement('a')
  a.dataset.index = index
  if (item.type !== 'VIDEO') a.href = item.previewUrl
  a.style.left = l.left + 'px'
  a.style.top = l.top + 'px'
  a.style.width = l.width + 'px'
  a.style.height = l.height + 'px'

  if (item.thumbhash) {
    const url = decodeThumbhash(item.thumbhash)
    if (url) a.style.backgroundImage = 'url(' + url + ')'
  }

  const img = document.createElement('img')
  img.alt = item.description || ''
  img.dataset.src = item.thumbnailUrl
  img.onerror = onThumbError
  a.appendChild(img)

  if (item.type === 'VIDEO') {
    const playIcon = document.createElement('div')
    playIcon.className = 'play-icon'
    a.appendChild(playIcon)
  }

  if (state.toolbarEl) {
    const check = document.createElement('div')
    check.className = 'tile-check'
    check.innerHTML = CHECK_SVG
    check.addEventListener('click', (e) => {
      e.preventDefault()
      e.stopPropagation()
      if (!state.selectMode) enterSelectMode(state)
      toggleSelection(state, item.id)
    })
    a.appendChild(check)
    if (state.selected.has(item.id)) a.classList.add('selected')
    attachLongPress(state, a, item.id)
  }

  a.addEventListener('click', (e) => {
    e.preventDefault()
    if (state.selectMode) {
      toggleSelection(state, item.id)
    } else {
      openLightbox(state, index)
    }
  })

  return a
}

function attachLongPress (state, tile, id) {
  let timer = null
  let pressed = false
  const cancel = () => {
    if (timer) { clearTimeout(timer); timer = null }
  }
  tile.addEventListener('pointerdown', (e) => {
    if (e.button !== undefined && e.button !== 0) return
    pressed = false
    timer = setTimeout(() => {
      timer = null
      pressed = true
      if (!state.selectMode) enterSelectMode(state)
      toggleSelection(state, id)
    }, LONG_PRESS_MS)
  })
  tile.addEventListener('pointerup', cancel)
  tile.addEventListener('pointercancel', cancel)
  tile.addEventListener('pointerleave', cancel)
  tile.addEventListener('pointermove', (e) => {
    if (Math.abs(e.movementX) + Math.abs(e.movementY) > 6) cancel()
  })
  tile.addEventListener('click', (e) => {
    if (pressed) {
      pressed = false
      e.preventDefault()
      e.stopImmediatePropagation()
    }
  }, true)
}

function enterSelectMode (state) {
  if (state.selectMode) return
  state.selectMode = true
  if (state.container) state.container.classList.add('select-mode')
  if (state.toolbarEl) state.toolbarEl.hidden = false
}

function exitSelectMode (state) {
  if (!state.selectMode) return
  state.selectMode = false
  for (const a of state.renderedTiles.values()) a.classList.remove('selected')
  state.selected.clear()
  if (state.container) state.container.classList.remove('select-mode')
  if (state.toolbarEl) state.toolbarEl.hidden = true
  updateSelectionUI(state)
}

function toggleSelection (state, id) {
  if (state.selected.has(id)) state.selected.delete(id)
  else state.selected.add(id)
  const idx = state.items.findIndex(it => it.id === id)
  const tile = idx >= 0 ? state.renderedTiles.get(idx) : null
  if (tile) tile.classList.toggle('selected', state.selected.has(id))
  if (state.selected.size === 0 && state.selectMode) exitSelectMode(state)
  else updateSelectionUI(state)
}

function updateSelectionUI (state) {
  if (state.countEl) state.countEl.textContent = state.selected.size + ' selected'
  if (state.downloadBtn) state.downloadBtn.disabled = state.selected.size === 0
  if (state.selectAllBtn) {
    state.selectAllBtn.textContent =
      state.selected.size === state.items.length ? 'Deselect all' : 'Select all'
  }
}

function selectAllOrNone (state) {
  if (state.selected.size === state.items.length) {
    for (const a of state.renderedTiles.values()) a.classList.remove('selected')
    state.selected.clear()
    updateSelectionUI(state)
  } else {
    if (!state.selectMode) enterSelectMode(state)
    for (const item of state.items) state.selected.add(item.id)
    for (const a of state.renderedTiles.values()) a.classList.add('selected')
    updateSelectionUI(state)
  }
}

function downloadSelected (state) {
  if (state.selected.size === 0) return
  const form = document.createElement('form')
  form.method = 'POST'
  form.action = window.location.pathname + '/download'
  const input = document.createElement('input')
  input.type = 'hidden'
  input.name = 'assets'
  input.value = JSON.stringify(Array.from(state.selected))
  form.appendChild(input)
  document.body.appendChild(form)
  form.submit()
  form.remove()
}

function getVisibleRange (state) {
  const containerTop = state.container.getBoundingClientRect().top
  const viewportTopInContainer = -containerTop
  const viewportBottomInContainer = viewportTopInContainer + window.innerHeight
  const buffer = window.innerHeight * BUFFER_VIEWPORTS
  return {
    top: viewportTopInContainer - buffer,
    bottom: viewportBottomInContainer + buffer
  }
}

function createHeader (header) {
  const el = document.createElement('h2')
  el.className = 'group-header'
  el.style.top = header.top + 'px'
  el.textContent = header.label
  return el
}

function virtualize (state) {
  if (!state.container || !state.layout.length) return
  const { top, bottom } = getVisibleRange(state)

  const neededTiles = new Set()
  for (const l of state.layout) {
    if (!l) continue
    if (l.top + l.height < top) continue
    if (l.top > bottom) break
    neededTiles.add(l.index)
  }
  for (const [index, el] of state.renderedTiles) {
    if (!neededTiles.has(index)) {
      el.remove()
      state.renderedTiles.delete(index)
    }
  }
  for (const index of neededTiles) {
    if (state.renderedTiles.has(index)) continue
    const tile = createTile(state, index)
    state.container.appendChild(tile)
    state.renderedTiles.set(index, tile)
  }

  const neededHeaders = new Set()
  for (const h of state.headers) {
    if (h.top + h.height < top) continue
    if (h.top > bottom) break
    neededHeaders.add(h.label)
  }
  for (const [label, el] of state.renderedHeaders) {
    if (!neededHeaders.has(label)) {
      el.remove()
      state.renderedHeaders.delete(label)
    }
  }
  for (const label of neededHeaders) {
    if (state.renderedHeaders.has(label)) continue
    const h = state.headers.find(x => x.label === label)
    if (!h) continue
    const el = createHeader(h)
    state.container.appendChild(el)
    state.renderedHeaders.set(label, el)
  }
}

function loadVisibleTiles (state) {
  if (!state.container) return
  const vpHeight = window.innerHeight
  const containerTop = state.container.getBoundingClientRect().top
  for (const a of state.renderedTiles.values()) {
    const img = a.firstElementChild
    if (!(img instanceof HTMLImageElement)) continue
    const aTopInVp = containerTop + parseFloat(a.style.top || '0')
    const aHeight = parseFloat(a.style.height || '0')
    const isFar = aTopInVp + aHeight < -IMAGE_LOAD_MARGIN_PX ||
      aTopInVp > vpHeight + IMAGE_LOAD_MARGIN_PX
    if (isFar) {
      if (img.src && !img.complete) {
        img.dataset.src = img.src
        img.removeAttribute('src')
      }
    } else if (img.dataset.src) {
      img.src = img.dataset.src
      img.removeAttribute('data-src')
    }
  }
}

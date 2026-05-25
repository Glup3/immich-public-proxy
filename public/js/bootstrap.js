// @ts-check

import { createGalleryState } from './gallery-state.js'
import { initLightbox, openLightbox } from './lightbox.js'
import { computeLayoutAndRender, onScroll, setupToolbar } from './virtualization.js'

function readInitParams () {
  const el = document.getElementById('ipp-init')
  if (!el) return {}
  try { return JSON.parse(el.textContent || '{}') } catch (e) { return {} }
}

export function initGallery () {
  const params = readInitParams()
  const state = createGalleryState()
  state.items = params.items || []
  state.lightboxConfig = params.lightboxConfig || {}
  state.groupByDate = !!params.groupByDate
  state.container = document.getElementById('gallery')
  if (!state.container) return

  setupToolbar(state)
  initLightbox(state)
  bindTravelOpeners(state)

  let resizeFrame
  const resizeObserver = new ResizeObserver(() => {
    if (resizeFrame) cancelAnimationFrame(resizeFrame)
    resizeFrame = requestAnimationFrame(() => computeLayoutAndRender(state))
  })
  resizeObserver.observe(state.container)

  window.addEventListener('scroll', () => onScroll(state), { passive: true })

  const hash = window.location.hash.slice(1)
  if (hash) {
    const idx = state.items.findIndex(it => it.id === hash)
    if (idx >= 0) openLightbox(state, idx)
  } else if (params.openItem && params.openItem > 0 && params.openItem <= state.items.length) {
    openLightbox(state, params.openItem - 1)
  }
}

function bindTravelOpeners (state) {
  const buttons = document.querySelectorAll('[data-open-asset]')
  buttons.forEach(el => {
    el.addEventListener('click', () => {
      const id = el.getAttribute('data-open-asset')
      const idx = state.items.findIndex(it => it.id === id)
      if (idx >= 0) openLightbox(state, idx)
    })
  })
}

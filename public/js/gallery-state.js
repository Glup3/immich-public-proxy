// @ts-check

// Justified-rows target row height (matches Immich's main view)
export const TARGET_ROW_HEIGHT = 235
export const GAP = 4
export const MOBILE_BREAKPOINT = 640
export const MOBILE_COLS = 3
export const IMAGE_LOAD_MARGIN_PX = 300
export const SCROLL_SETTLE_MS = 100
export const BUFFER_VIEWPORTS = 1
export const HEADER_HEIGHT = 48
export const GROUP_GAP = 16
export const LONG_PRESS_MS = 500

export const ICON_DOWNLOAD = '<svg class="pswp__icn" viewBox="0 0 24 24" aria-hidden="true"><path fill="currentColor" d="M5,20H19V18H5M19,9H15V3H9V9H5L12,16L19,9Z"/></svg>'
export const CHECK_SVG = '<svg viewBox="0 0 24 24" aria-hidden="true"><path fill="currentColor" d="M21,7L9,19L3.5,13.5L4.91,12.09L9,16.17L19.59,5.59L21,7Z"/></svg>'

export function createGalleryState () {
  return {
    items: [],
    layout: [],
    headers: [],
    groupByDate: false,
    container: null,
    lightbox: null,
    lightboxConfig: {},
    renderedTiles: new Map(),
    renderedHeaders: new Map(),
    lastContainerW: 0,
    selectMode: false,
    selected: new Set(),
    toolbarEl: null,
    countEl: null,
    selectAllBtn: null,
    cancelBtn: null,
    downloadBtn: null,
    scrollFrame: null,
    loadTimer: null
  }
}

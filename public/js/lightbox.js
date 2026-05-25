// @ts-check

// @ts-ignore
import PhotoSwipeLightbox from '/assets/photoswipe/photoswipe-lightbox.esm.js'
import { ICON_DOWNLOAD } from './gallery-state.js'

export function initLightbox (state) {
  state.lightbox = new PhotoSwipeLightbox({
    dataSource: buildDataSource(state.items),
    pswpModule: () => import('/assets/photoswipe/photoswipe.esm.js'),
    bgOpacity: 1,
    showHideAnimationType: 'fade',
    closeOnVerticalDrag: true,
    arrowKeys: true,
    loop: false,
    padding: { top: 56, bottom: 56, left: 16, right: 16 }
  })

  if (state.lightboxConfig.showDownload) {
    state.lightbox.on('uiRegister', () => {
      state.lightbox.pswp.ui.registerElement({
        name: 'download-button',
        order: 8,
        isButton: true,
        tagName: 'a',
        ariaLabel: 'Download',
        html: ICON_DOWNLOAD,
        onInit: (el, pswp) => {
          el.setAttribute('target', '_blank')
          el.setAttribute('rel', 'noopener')
          const update = () => {
            const item = state.items[pswp.currIndex]
            if (item && item.downloadUrl) {
              el.href = item.downloadUrl
              el.setAttribute('download', item.downloadFilename || '')
            } else {
              el.removeAttribute('href')
            }
          }
          update()
          pswp.on('change', update)
        }
      })
    })
  }

  state.lightbox.on('uiRegister', () => {
    const pswp = state.lightbox.pswp
    pswp.on('change', () => {
      const item = state.items[pswp.currIndex]
      if (item) history.replaceState(null, '', '#' + item.id)
    })
    pswp.on('close', () => {
      history.replaceState(null, '', window.location.pathname + window.location.search)
      scrollToCurrentSlide(state, pswp.currIndex)
    })
  })

  const docEl = document.documentElement
  if (!state.lightboxConfig.showArrows) docEl.classList.add('pswp-no-arrows')
  if (!state.lightboxConfig.mobileArrows) docEl.classList.add('pswp-no-mobile-arrows')

  state.lightbox.init()
}

export function openLightbox (state, index) {
  if (state.lightbox) state.lightbox.loadAndOpen(index)
}

function parseVideoData (item) {
  try {
    const data = JSON.parse(item.videoData || '{}')
    const source = (data.source && data.source[0]) || {}
    return { src: source.src || '', type: source.type || 'video/mp4' }
  } catch (e) {
    return { src: '', type: 'video/mp4' }
  }
}

function escapeAttr (s) {
  return String(s).replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

function buildDataSource (items) {
  return items.map(item => {
    if (item.type === 'VIDEO') {
      const v = parseVideoData(item)
      return {
        html:
          '<div class="pswp__video-wrap">' +
            '<video controls playsinline poster="' + escapeAttr(item.thumbnailUrl) + '">' +
              '<source src="' + escapeAttr(v.src) + '" type="' + escapeAttr(v.type) + '">' +
            '</video>' +
          '</div>'
      }
    }
    return {
      src: item.previewUrl,
      width: item.width || 1600,
      height: item.height || 1200,
      msrc: item.thumbnailUrl,
      alt: item.description || ''
    }
  })
}

function scrollToCurrentSlide (state, index) {
  if (!state.container || index == null || !state.layout[index]) return
  const entry = state.layout[index]
  const containerTop = state.container.getBoundingClientRect().top + window.scrollY
  const tileTop = containerTop + entry.top
  const tileBottom = tileTop + entry.height
  const viewportTop = window.scrollY
  const viewportBottom = viewportTop + window.innerHeight
  if (tileTop >= viewportTop && tileBottom <= viewportBottom) return
  const targetScroll = tileTop - (window.innerHeight - entry.height) / 2
  window.scrollTo({ top: Math.max(0, targetScroll), behavior: 'instant' })
}

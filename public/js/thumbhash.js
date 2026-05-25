// @ts-check

// @ts-ignore
import { thumbHashToDataURL } from '/assets/thumbhash/thumbhash.js'

const thumbhashCache = new Map()

export function decodeThumbhash (base64) {
  const cached = thumbhashCache.get(base64)
  if (cached) return cached
  try {
    const binary = atob(base64)
    const bytes = new Uint8Array(binary.length)
    for (let i = 0; i < bytes.length; i++) bytes[i] = binary.charCodeAt(i)
    const url = thumbHashToDataURL(bytes)
    thumbhashCache.set(base64, url)
    return url
  } catch (e) {
    return null
  }
}

// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// v-scroll-thumb: no scrollbar on a widget's list, only a small position indicator over it while it scrolls, as on the phone.
import type { Directive } from 'vue'

interface Held { thumb: HTMLElement; onScroll: () => void; timer: number }
const held = new WeakMap<HTMLElement, Held>()

export const vScrollThumb: Directive<HTMLElement> = {
  mounted(el) {
    const box = el.parentElement!
    if (getComputedStyle(box).position === 'static') box.style.position = 'relative'
    el.classList.add('ph-scrollthumb-list')
    const thumb = document.createElement('i')
    thumb.className = 'ph-scrollthumb'
    thumb.setAttribute('aria-hidden', 'true')
    box.appendChild(thumb)
    const h: Held = { thumb, timer: 0, onScroll() {
      const room = el.scrollHeight - el.clientHeight
      if (room <= 0) return
      const size = Math.max(20, el.clientHeight * el.clientHeight / el.scrollHeight)
      thumb.style.height = size + 'px'
      thumb.style.top = el.offsetTop + (el.clientHeight - size) * el.scrollTop / room + 'px'
      thumb.classList.add('is-on')
      clearTimeout(h.timer)
      h.timer = window.setTimeout(() => thumb.classList.remove('is-on'), 700)
    } }
    held.set(el, h)
    el.addEventListener('scroll', h.onScroll, { passive: true })
  },
  unmounted(el) {
    const h = held.get(el)
    if (!h) return
    clearTimeout(h.timer)
    el.removeEventListener('scroll', h.onScroll)
    h.thumb.remove()
  },
}

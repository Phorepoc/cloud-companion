<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->
<!-- One window of the desktop view: the Etesian header bar (one 44 px row of chrome: the section's lit squircle, its name in light type,
     a drag area, then minimise / maximise / close) above the section's page. Dragging the bar moves the window; dragging it to the left
     or right edge of the desktop snaps it to that half, to the top edge maximises it (the zone shows while dragging). Double-clicking the
     bar maximises or restores. Any edge or corner resizes. The page runs in its own Quasar layout, so it behaves as it does full screen.
     Geometry lives in the desktop (DesktopView.vue); this component reports moves and snaps. -->
<template>
  <section class="ph-win" :class="{ 'is-focused': focused, 'is-tiled': win.state !== 'float', 'is-dragging': dragging }"
           :style="style" role="region" :aria-label="section.label + ' window'" :data-test="'win-' + section.id"
           @pointerdown.capture="$emit('focus')">
    <header class="ph-win__bar" @pointerdown="startDrag" @dblclick="$emit('maximise')">
      <SectionTile :section="section" :size="26" />
      <h2 class="ph-win__title">{{ section.label }}</h2>
      <!-- the page's own buttons (Browser, Android), up to the separator before minimise / maximise / close -->
      <div ref="actions" class="ph-win__actions" data-test="title-actions" />
      <div class="ph-win__ctl">
        <button type="button" class="ph-win__btn hd-sq" aria-label="Minimise" title="Minimise" @click="$emit('minimise')">
          <q-icon name="sym_o_minimize" size="20px" aria-hidden="true" />
        </button>
        <button type="button" class="ph-win__btn hd-sq" :aria-label="win.state === 'max' ? 'Restore' : 'Maximise'"
                :title="win.state === 'max' ? 'Restore' : 'Maximise'" @click="$emit('maximise')">
          <q-icon :name="win.state === 'max' ? 'sym_o_filter_none' : 'sym_o_crop_square'" size="18px" aria-hidden="true" />
        </button>
        <button type="button" class="ph-win__btn ph-win__close hd-sq" aria-label="Close" title="Close" data-test="win-close" @click="$emit('close')">
          <q-icon name="sym_o_close" size="20px" aria-hidden="true" />
        </button>
      </div>
    </header>
    <div class="ph-win__body">
      <q-layout view="hhh lpr fff" container class="ph-win__layout ph-in-window">
        <q-page-container>
          <component :is="page" v-bind="pageProps" />
        </q-page-container>
      </q-layout>
      <!-- while the desktop moves or resizes windows, pages (the browser canvas) must not take the pointer -->
      <div v-if="busy" class="ph-win__shield" />
    </div>
    <template v-if="win.state === 'float'">
      <span v-for="h in HANDLES" :key="h" class="ph-win__rz" :class="'is-' + h" aria-hidden="true" @pointerdown.stop="startResize($event, h)" />
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, provide, ref, type Component } from 'vue'
import { TITLE_ACTIONS } from '../components/titlebar'
import type { Section } from '../nav'
import type { Rect, Win } from './types'
import SectionTile from '../components/SectionTile.vue'

const props = withDefaults(defineProps<{
  win: Win            // the stored window (its floating rectangle and state)
  rect: Rect          // where it is drawn now (the desktop resolves snapped states)
  section: Section
  page: Component
  pageProps?: Record<string, unknown>
  focused?: boolean
  busy?: boolean      // some window is being moved or resized
  card?: { x: number; y: number; s: number; z: number; side: boolean } | null // in the Windows view: the window itself, scaled into its card
}>(), { pageProps: () => ({}), card: null })
const emit = defineEmits(['focus', 'move', 'resize', 'drag-zone', 'drop', 'minimise', 'maximise', 'close', 'busy'])

// pages can tell they are in a window (Android uses its compact controls there)
provide('phInWindow', true)
const actions = ref<HTMLElement | null>(null)
provide(TITLE_ACTIONS, actions)

const HANDLES = ['n', 's', 'e', 'w', 'ne', 'nw', 'se', 'sw']
const MIN_W = 360
const MIN_H = 240
const dragging = ref(false)

const style = computed(() => {
  const c = props.card
  if (c) return { left: c.x + 'px', top: c.y + 'px', width: props.rect.w + 'px', height: props.rect.h + 'px', zIndex: c.z,
    transform: `scale(${c.s})`, transformOrigin: '0 0', opacity: c.side ? 0.5 : 1, transition: 'left .18s, top .18s, transform .18s, opacity .18s' }
  return { left: props.rect.x + 'px', top: props.rect.y + 'px', width: props.rect.w + 'px', height: props.rect.h + 'px', zIndex: props.win.z }
})

/** Moves with the pointer; leaving a snapped state restores the floating size under the pointer. */
function startDrag(e: PointerEvent) {
  if (e.button !== 0 || (e.target as Element).closest('button')) return
  const bar = e.currentTarget as HTMLElement
  bar.setPointerCapture(e.pointerId)
  let { x, y } = props.rect
  let gx = e.clientX - x
  const gy = e.clientY - y
  let moved = false
  const move = (ev: PointerEvent) => {
    if (!moved && Math.hypot(ev.clientX - e.clientX, ev.clientY - e.clientY) < 4) return
    if (!moved) {
      moved = true; dragging.value = true; emit('busy', true)
      // a snapped or maximised window comes off at its floating size, keeping the pointer at the same share of the bar
      if (props.win.state !== 'float') gx = (gx / props.rect.w) * props.win.w
    }
    x = ev.clientX - gx; y = ev.clientY - gy
    emit('move', { x, y })
    emit('drag-zone', { px: ev.clientX, py: ev.clientY })
  }
  const up = (ev: PointerEvent) => {
    bar.removeEventListener('pointermove', move); bar.removeEventListener('pointerup', up); bar.removeEventListener('pointercancel', up)
    if (moved) { dragging.value = false; emit('busy', false); emit('drop', { px: ev.clientX, py: ev.clientY }) }
  }
  bar.addEventListener('pointermove', move); bar.addEventListener('pointerup', up); bar.addEventListener('pointercancel', up)
}

function startResize(e: PointerEvent, h: string) {
  if (e.button !== 0) return
  const el = e.currentTarget as HTMLElement
  el.setPointerCapture(e.pointerId)
  const r0 = { ...props.rect }
  emit('busy', true)
  const move = (ev: PointerEvent) => {
    const dx = ev.clientX - e.clientX
    const dy = ev.clientY - e.clientY
    const r = { ...r0 }
    if (h.includes('e')) r.w = Math.max(MIN_W, r0.w + dx)
    if (h.includes('s')) r.h = Math.max(MIN_H, r0.h + dy)
    if (h.includes('w')) { r.w = Math.max(MIN_W, r0.w - dx); r.x = r0.x + r0.w - r.w }
    if (h.includes('n')) { r.h = Math.max(MIN_H, r0.h - dy); r.y = r0.y + r0.h - r.h }
    emit('resize', r)
  }
  const up = () => {
    el.removeEventListener('pointermove', move); el.removeEventListener('pointerup', up); el.removeEventListener('pointercancel', up)
    emit('busy', false)
  }
  el.addEventListener('pointermove', move); el.addEventListener('pointerup', up); el.addEventListener('pointercancel', up)
}
</script>

<style scoped>
.ph-win {
  position: absolute; display: flex; flex-direction: column; overflow: hidden;
  background: var(--ph-bg); border-radius: var(--ph-r-card);
  box-shadow: 0 0 0 1px rgba(0, 0, 0, 0.5), 0 6px 18px rgba(0, 0, 0, 0.35);
  transition: box-shadow var(--ph-dur-1) var(--ph-ease);
}
.ph-win.is-focused { box-shadow: 0 0 0 1px rgba(0, 0, 0, 0.6), var(--hd-shadow-overlay); }
.ph-win.is-tiled { border-radius: 6px; }
/* the header bar: N9 chrome, one row */
.ph-win__bar {
  flex: none; display: flex; align-items: center; gap: var(--ph-s2); height: 44px; padding: 0 var(--ph-s1) 0 var(--ph-s3);
  background: var(--ph-chrome); color: var(--ph-chrome-fg); cursor: default; user-select: none; touch-action: none;
}
.ph-win.is-dragging .ph-win__bar { cursor: grabbing; }
.ph-win__title { flex: 1; min-width: 0; font: var(--hd-font-weight-light) var(--hd-font-size-xlarge)/1 var(--ph-font-light); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
/* inactive windows: header content at half strength */
.ph-win:not(.is-focused) .ph-win__bar > * { opacity: 0.5; }
.ph-win__actions { flex: none; display: flex; align-items: center; gap: 2px; padding-right: var(--ph-s2); }
.ph-win__actions:empty { display: none; }
.ph-win__ctl { display: flex; align-items: center; padding-left: var(--ph-s1); border-left: 1px solid var(--ph-chrome-divider); }
.ph-win__btn {
  --hd-sq-size: 34px; width: 44px; height: 44px; display: grid; place-items: center;
  border: 0; color: inherit; cursor: pointer; font: inherit;
}
.ph-win__btn:hover { --hd-sq-fill: rgba(255, 255, 255, 0.14); }
.ph-win__btn:active { --hd-sq-fill: rgba(255, 255, 255, 0.24); }
.ph-win__close:hover, .ph-win__close:active { --hd-sq-fill: var(--hd-color-palette-warning); color: #fff; }
/* a size container: pages that fit themselves to the screen height (Android) use the window's height instead (cqh) */
.ph-win__body { position: relative; flex: 1; min-height: 0; container-type: size; }
.ph-win__layout { height: 100%; }
.ph-win__shield { position: absolute; inset: 0; z-index: 10; }
/* the header bar names the window: the page's own heading stays for assistive technology only (no duplicated title) */
.ph-win__body :deep(.q-page h1) {
  position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap;
}
/* resize handles: invisible strips along the edges, a little larger at the corners */
.ph-win__rz { position: absolute; z-index: 20; }
.ph-win__rz.is-n, .ph-win__rz.is-s { left: 12px; right: 12px; height: 8px; cursor: ns-resize; }
.ph-win__rz.is-n { top: -2px; }
.ph-win__rz.is-s { bottom: -2px; }
.ph-win__rz.is-e, .ph-win__rz.is-w { top: 12px; bottom: 12px; width: 8px; cursor: ew-resize; }
.ph-win__rz.is-e { right: -2px; }
.ph-win__rz.is-w { left: -2px; }
.ph-win__rz.is-ne, .ph-win__rz.is-nw, .ph-win__rz.is-se, .ph-win__rz.is-sw { width: 16px; height: 16px; }
.ph-win__rz.is-ne { top: -2px; right: -2px; cursor: nesw-resize; }
.ph-win__rz.is-sw { bottom: -2px; left: -2px; cursor: nesw-resize; }
.ph-win__rz.is-nw { top: -2px; left: -2px; cursor: nwse-resize; }
.ph-win__rz.is-se { bottom: -2px; right: -2px; cursor: nwse-resize; }
</style>

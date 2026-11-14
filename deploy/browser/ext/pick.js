// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only
// A page's "choose a file" opens the picker of the device you are on (through the Browser page), not one of the server's disk.
// (in a block: the content scripts share one scope)
{
let n = 0
const waiting = new Map()
function ask(input) {
  const id = location.href.length + '-' + Date.now() + '-' + ++n
  waiting.set(id, input)
  browser.runtime.sendMessage({ pick: { id, accept: input.accept || '', multiple: input.multiple } })
}
document.addEventListener('click', (e) => {
  const el = e.target instanceof Element && e.target.closest('input[type=file], label')
  let input = el
  if (el && el.tagName === 'LABEL') input = el.control
  if (!input || input.type !== 'file' || input.disabled) return
  e.preventDefault(); e.stopImmediatePropagation()
  ask(input)
}, true)
// a script's input.click() and showPicker()
const P = window.wrappedJSObject.HTMLInputElement.prototype
const click = P.click, show = P.showPicker
P.click = exportFunction(function () { if (this.type === 'file') ask(this); else click.call(this) }, window)
if (show) P.showPicker = exportFunction(function () { if (this.type === 'file') ask(this); else show.call(this) }, window)
browser.runtime.onMessage.addListener((m) => {
  if (!m.files) return
  const input = waiting.get(m.id)
  waiting.delete(m.id)
  if (!input) return
  if (!m.files.length) { input.dispatchEvent(new Event('cancel', { bubbles: true })); return }
  const dt = new DataTransfer()
  for (const f of m.files) dt.items.add(new File([f.data], f.name, { type: f.type }))
  input.files = dt.files
  input.dispatchEvent(new Event('input', { bubbles: true }))
  input.dispatchEvent(new Event('change', { bubbles: true }))
})
}

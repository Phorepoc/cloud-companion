// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
// The privileged half of the bridge (a WebExtension experiment, allowed by deploy/browser/mozilla.cfg): what the phone link needs and
// the WebExtension APIs do not give: a tap Firefox itself delivers (isTrusted), and the "continue anyway" of a refused certificate.
/* global ExtensionAPI, ChromeUtils */
const { ExtensionError } = ChromeUtils.importESModule('resource://gre/modules/ExtensionUtils.sys.mjs').ExtensionUtils
const { setTimeout, clearTimeout } = ChromeUtils.importESModule('resource://gre/modules/Timer.sys.mjs')
this.phe = class extends ExtensionAPI {
  getAPI(context) {
    const browserOf = (tabId) => {
      const t = context.extension.tabManager.get(tabId)
      if (!t) throw new Error('no such tab')
      return t.nativeTab.linkedBrowser
    }
    // what a function throws reaches the extension as "An unexpected error occurred" unless it is an ExtensionError
    const guard = (f) => async (...a) => { try { return await f(...a) } catch (e) { throw new ExtensionError(String(e)) } }
    // code run in the tab's content process with system privileges (a frame script); it answers with sendAsyncMessage('phe:r', value)
    const inContent = (tabId, code) => {
      const mm = browserOf(tabId).messageManager
      return new Promise((res) => {
        const done = (v) => { mm.removeMessageListener('phe:r', l); clearTimeout(tm); res(v) }
        const l = (m) => done(m.data)
        const tm = setTimeout(() => done('timeout'), 5000)
        mm.addMessageListener('phe:r', l)
        mm.loadFrameScript('data:application/javascript,' + encodeURIComponent('try { ' + code + ' } catch (e) { sendAsyncMessage("phe:r", "error " + e) }'), false)
      })
    }
    // The tap, as a finger makes it: pointer, mouse and click events through the page's own event path (the presshell), which
    // Firefox treats as its own input: isTrusted is true, links follow, boxes tick, focus moves.
    const click = async (tabId, x, y) => {
      const r = await inContent(tabId, `
        const w = content, d = w.document, X = ${Number(x)}, Y = ${Number(y)}
        const e = d.elementFromPoint(X, Y)
        if (!e) { sendAsyncMessage('phe:r', 'no element') } else {
          const o = { bubbles: true, cancelable: true, composed: true, clientX: X, clientY: Y, screenX: X, screenY: Y, view: w, button: 0 }
          const p = Object.assign({ pointerId: 1, pointerType: 'touch', isPrimary: true, width: 1, height: 1 }, o)
          const go = (ev) => w.windowUtils.dispatchDOMEventViaPresShellForTesting(e, ev)
          const h = w.windowUtils.setHandlingUserInput(true) // a real tap is user input: downloads, popups and the clipboard need it
          try {
          go(new w.PointerEvent('pointerdown', Object.assign({ buttons: 1 }, p)))
          go(new w.MouseEvent('mousedown', Object.assign({ buttons: 1, detail: 1 }, o)))
          go(new w.PointerEvent('pointerup', p))
          go(new w.MouseEvent('mouseup', Object.assign({ detail: 1 }, o)))
          } finally { h.destruct() }
          sendAsyncMessage('phe:r', 'ok')
        }`)
      if (r !== 'ok') throw new Error('tap: ' + r)
      return true
    }
    const acceptCert = async (tabId) => {
      const r = await inContent(tabId, `
        const d = content.document
        if (!/^about:(net|cert)error/.test(d.documentURI)) sendAsyncMessage('phe:r', 'not an error page')
        else d.addCertException(true).then(() => sendAsyncMessage('phe:r', 'ok'), (e) => sendAsyncMessage('phe:r', 'error ' + e))`)
      return r
    }
    return { phe: { click: guard(click), acceptCert: guard(acceptCert) } }
  }
}

// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// A tiny IndexedDB key/value store for the device credentials ({dev, key}); localStorage is not used for the key.
function db() {
  return new Promise((resolve, reject) => {
    const r = indexedDB.open('phecloud', 1)
    r.onupgradeneeded = () => r.result.createObjectStore('kv')
    r.onsuccess = () => resolve(r.result)
    r.onerror = () => reject(r.error)
  })
}

export async function idbGet(key) {
  const d = await db()
  return new Promise((resolve, reject) => {
    const q = d.transaction('kv').objectStore('kv').get(key)
    q.onsuccess = () => resolve(q.result)
    q.onerror = () => reject(q.error)
  })
}

export async function idbSet(key, value) {
  const d = await db()
  return new Promise((resolve, reject) => {
    const tx = d.transaction('kv', 'readwrite')
    tx.objectStore('kv').put(value, key)
    tx.oncomplete = () => resolve()
    tx.onerror = () => reject(tx.error)
  })
}

export async function idbDel(key) {
  const d = await db()
  return new Promise((resolve, reject) => {
    const tx = d.transaction('kv', 'readwrite')
    tx.objectStore('kv').delete(key)
    tx.oncomplete = () => resolve()
    tx.onerror = () => reject(tx.error)
  })
}

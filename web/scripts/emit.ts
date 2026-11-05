// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0
//
// Compiles the two files served as plain JavaScript from their TypeScript (tsconfig.emit.json, tsconfig.sw.json) and puts them where they are served:
// the Android stream into cmd/pheandroid (the bridge embeds it for its own page) and the service worker into public/.
//   npm run emit             writes them
//   npm run emit -- --check  only says whether the committed ones are up to date (npm run check runs this)
import { execFileSync } from 'node:child_process'
import { existsSync, mkdtempSync, readFileSync, writeFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'

const web = join(import.meta.dirname, '..')
const out = mkdtempSync(join(tmpdir(), 'emit-'))
// [tsconfig, compiled file, where it goes]
const targets: [string, string, string][] = [['tsconfig.emit.json', 'stream.js', '../cmd/pheandroid/stream.js'], ['tsconfig.sw.json', 'sw.js', 'public/sw.js']]
const check = process.argv.includes('--check')
try {
  let stale = 0
  for (const [config, built, dest] of targets) {
    execFileSync(join(web, 'node_modules/.bin/tsc'), ['-p', join(web, config), '--outDir', out], { stdio: 'inherit' })
    const js = readFileSync(join(out, built), 'utf8')
    const path = join(web, dest)
    if (!existsSync(dirname(path))) { console.log(`${dest}: not in this checkout (the image build copies web/ only), not compared`); continue }
    let had = ''
    try { had = readFileSync(path, 'utf8') } catch { /* not there yet */ }
    if (js === had) continue
    if (check) { console.error(`${dest} is out of date with its TypeScript: run npm run emit`); stale++ } else { writeFileSync(path, js); console.log('wrote ' + dest) }
  }
  if (stale) process.exit(1)
} finally { rmSync(out, { recursive: true, force: true }) }

#!/usr/bin/env node
// Copies the stroke-order data for exactly the characters the authored content
// uses from hanzi-writer-data into web/public/hanzi/, together with the licence
// the data is distributed under.
//
// Why copy instead of fetching from a CDN at runtime: the app must work offline
// and must not depend on a third party's uptime for a core exercise. Why only the
// characters in use: the full dataset is ~9,000 files and would dominate the
// bundle.
//
// Licensing: the stroke data derives from fonts by Arphic Technology Co., Ltd. and
// is distributed under the Arphic Public License, which permits commercial use but
// requires the licence text to travel with the data and modifications to be
// marked. That is why ARPHICPL.TXT is copied next to the data and referenced in
// the app footer.

import { copyFileSync, existsSync, mkdirSync, readFileSync, readdirSync, statSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const webDir = join(here, '..')
const repoRoot = join(webDir, '..')
const contentDir = join(repoRoot, 'content')
const dataPkg = join(webDir, 'node_modules', 'hanzi-writer-data')
const outDir = join(webDir, 'public', 'hanzi')

function yamlFiles(dir) {
  if (!existsSync(dir)) return []
  return readdirSync(dir).flatMap((entry) => {
    const full = join(dir, entry)
    if (statSync(full).isDirectory()) return yamlFiles(full)
    return full.endsWith('.yaml') || full.endsWith('.yml') ? [full] : []
  })
}

function charactersInUse() {
  const found = new Set()
  // Authors write `character: 你` in hanzi exercises and passages use `zh:` text;
  // only the explicit character fields need stroke data.
  const re = /^\s*character:\s*(\S)\s*$/gm
  for (const file of yamlFiles(contentDir)) {
    const text = readFileSync(file, 'utf8')
    for (const match of text.matchAll(re)) found.add(match[1])
  }
  return [...found].sort()
}

function dataFileFor(character) {
  for (const candidate of [join(dataPkg, `${character}.json`), join(dataPkg, 'data', `${character}.json`)]) {
    if (existsSync(candidate)) return candidate
  }
  return null
}

function main() {
  if (!existsSync(dataPkg)) {
    console.error('hanzi-writer-data is not installed. Run `pnpm install` first.')
    process.exit(1)
  }
  const characters = charactersInUse()
  if (characters.length === 0) {
    console.error('No `character:` fields found in content/ — nothing to copy. Is the content directory in place?')
    process.exit(1)
  }

  mkdirSync(outDir, { recursive: true })
  const missing = []
  for (const character of characters) {
    const source = dataFileFor(character)
    if (!source) {
      missing.push(character)
      continue
    }
    copyFileSync(source, join(outDir, `${character}.json`))
  }

  const licenseCandidates = [join(dataPkg, 'ARPHICPL.TXT'), join(repoRoot, 'node_modules', 'hanzi-writer-data', 'ARPHICPL.TXT')]
  const license = licenseCandidates.find((p) => existsSync(p))
  if (license) {
    copyFileSync(license, join(outDir, 'LICENSE.txt'))
  } else {
    writeFileSync(
      join(outDir, 'LICENSE.txt'),
      'The stroke-order data in this directory derives from the Make Me a Hanzi project and is\n' +
        'distributed under the Arphic Public License (Arphic Technology Co., Ltd.). The licence\n' +
        'text was not found inside hanzi-writer-data; see https://github.com/chanind/hanzi-writer-data.\n',
    )
  }

  console.log(`copied stroke data for ${characters.length - missing.length} characters to web/public/hanzi`)
  console.log(`characters: ${characters.join(' ')}`)
  if (missing.length) {
    console.error(`MISSING stroke data for: ${missing.join(' ')} — a hanzi exercise would render without its grid`)
    process.exit(1)
  }
}

main()

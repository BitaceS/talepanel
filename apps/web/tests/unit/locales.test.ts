import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

// The UI messages: locales/<code>/<area>.json.
const dir = join(__dirname, '../../locales')
const codes = readdirSync(dir)

type Tree = { [k: string]: string | Tree }

function flatten(tree: Tree, prefix = ''): Record<string, string> {
  const out: Record<string, string> = {}
  for (const [k, v] of Object.entries(tree)) {
    if (typeof v === 'string') out[prefix + k] = v
    else Object.assign(out, flatten(v, `${prefix}${k}.`))
  }
  return out
}

function messages(code: string): Record<string, string> {
  const all: Record<string, string> = {}
  for (const file of readdirSync(join(dir, code))) {
    Object.assign(all, flatten(JSON.parse(readFileSync(join(dir, code, file), 'utf8')) as Tree))
  }
  return all
}

describe('locales', () => {
  const en = messages('en')

  it('has English and German', () => {
    expect(codes.sort()).toEqual(['de', 'en'])
  })

  for (const code of codes.filter(c => c !== 'en')) {
    it(`${code} has exactly the English keys`, () => {
      const other = messages(code)
      expect(Object.keys(other).filter(k => !(k in en))).toEqual([])
      expect(Object.keys(en).filter(k => !(k in other))).toEqual([])
    })
  }

  // The build (unplugin-vue-i18n) refuses messages that look like HTML.
  for (const code of codes) {
    it(`${code} messages contain no HTML-like tags`, () => {
      const bad = Object.entries(messages(code)).filter(([, v]) => /<[^>]*>/.test(v)).map(([k]) => k)
      expect(bad).toEqual([])
    })
  }
})

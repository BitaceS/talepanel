import { describe, expect, it } from 'vitest'
import { detectIndent, findLossyNumbers, parseConfig, serializeConfig } from '../../utils/configForm'

describe('detectIndent', () => {
  it('uses the first indented line', () => {
    expect(detectIndent('{\n    "a": 1\n}\n')).toBe('    ')
    expect(detectIndent('{\n\t"a": 1\n}')).toBe('\t')
  })

  it('handles CRLF files', () => {
    expect(detectIndent('{\r\n  "a": 1\r\n}\r\n')).toBe('  ')
  })

  it('is not fooled by a blank line before a deeper level', () => {
    const raw = '{\n  "a": {\n\n      "b": 1\n  }\n}\n'
    expect(detectIndent(raw)).toBe('  ')
  })

  it('falls back to two spaces', () => {
    expect(detectIndent('{"a":1}')).toBe('  ')
  })
})

describe('findLossyNumbers', () => {
  it('flags integers beyond 2^53', () => {
    expect(findLossyNumbers('{"id": 9007199254740993}')).toEqual(['9007199254740993'])
  })

  it('ignores numbers inside strings and safe numbers', () => {
    expect(findLossyNumbers('{"a": "9007199254740993", "b": 1.5, "c": -3}')).toEqual([])
  })

  it('flags numbers that overflow to Infinity (they would be written as null)', () => {
    expect(findLossyNumbers('{"a": 1e400}')).toEqual(['1e400'])
  })

  it('flags big integers written with a fraction', () => {
    expect(findLossyNumbers('{"a": 12345678901234567890.5}')).toEqual(['12345678901234567890.5'])
  })
})

describe('serializeConfig', () => {
  it('keeps indent, CRLF and the trailing newline', () => {
    const raw = '{\r\n    "a": 1\r\n}\r\n'
    const parsed = parseConfig(raw)
    expect(parsed.ok).toBe(true)
    if (parsed.ok) expect(serializeConfig(parsed.value, raw)).toBe(raw)
  })
})

describe('parseConfig', () => {
  it('reports invalid JSON', () => {
    expect(parseConfig('{"a": }').ok).toBe(false)
  })

  it('blocks the form for lossy numbers', () => {
    const r = parseConfig('{"a": 1e400}')
    expect(r.ok && r.formReason).toBeTruthy()
  })
})

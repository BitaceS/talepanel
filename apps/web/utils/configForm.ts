// Helpers for the server config editor (Config tab).
//
// The form never invents keys: it renders whatever the file on the node
// contains, so it keeps working when Hytale adds or renames settings. Known
// keys only get a friendlier label and a hint.

export type JsonValue = string | number | boolean | null | JsonValue[] | { [key: string]: JsonValue }
export type JsonObject = { [key: string]: JsonValue }

export interface ConfigFileInfo {
  label: string
  description: string
}

// Files the Config tab lists first, with a short description. Other *.json
// files in the server root are listed after these.
export const KNOWN_CONFIG_FILES: Record<string, ConfigFileInfo> = {
  'config.json': { label: 'Server settings', description: 'Name, MOTD, password, player limit, view distance and defaults.' },
  'permissions.json': { label: 'Permissions', description: 'Permission groups and which players belong to them.' },
  'whitelist.json': { label: 'Whitelist', description: 'Whether the whitelist is on and who is on it.' },
  'bans.json': { label: 'Bans', description: 'Banned players.' },
}

export interface FieldInfo {
  label?: string
  hint?: string
  options?: string[]
  // Item type for lists that may be empty. Without it an empty list gets no
  // add field: the form can't know what an entry looks like (bans.json holds
  // objects, and a string there would break the file).
  items?: 'string' | 'number'
}

// Keyed by "<file>:<dotted path>". Only keys we know from the Hytale server.
const KNOWN_FIELDS: Record<string, FieldInfo> = {
  'config.json:ServerName': { label: 'Server name' },
  'config.json:MOTD': { label: 'Message of the day', hint: 'Shown in the server list.' },
  'config.json:Password': { label: 'Join password', hint: 'Leave empty for no password.' },
  'config.json:MaxPlayers': { label: 'Max players' },
  'config.json:MaxViewRadius': { label: 'Max view radius', hint: 'In chunks. Higher values cost RAM and CPU.' },
  'config.json:Defaults.World': { label: 'Default world', hint: 'Can also be set on the Worlds tab.' },
  'config.json:Defaults.GameMode': { label: 'Default game mode', options: ['Adventure', 'Creative'] },
  'whitelist.json:enabled': { label: 'Whitelist enabled' },
  'whitelist.json:list': { label: 'Players', hint: 'Player UUIDs.', items: 'string' },
}

export function fieldInfo(file: string, path: string[]): FieldInfo {
  return KNOWN_FIELDS[`${file}:${path.join('.')}`] ?? {}
}

// "MaxViewRadius" → "Max view radius", "max_players" → "Max players".
export function humanizeKey(key: string): string {
  const spaced = key
    .replace(/[_-]+/g, ' ')
    .replace(/([a-z0-9])([A-Z])/g, '$1 $2')
    .replace(/([A-Z]+)([A-Z][a-z])/g, '$1 $2')
    .trim()
  if (!spaced) return key
  const lower = spaced.replace(/\b([A-Z][a-z]+)\b/g, w => w.toLowerCase())
  return lower.charAt(0).toUpperCase() + lower.slice(1)
}

export function isSecretKey(key: string): boolean {
  return /password|secret|token/i.test(key)
}

export function isPlainObject(v: unknown): v is JsonObject {
  return typeof v === 'object' && v !== null && !Array.isArray(v)
}

// Integer literals beyond 2^53 lose digits in JSON.parse, and the form would
// write the rounded value back. Literals that overflow to Infinity would even
// be written as null. Such files are only editable as raw text.
export function findLossyNumbers(raw: string): string[] {
  const withoutStrings = raw.replace(/"(?:[^"\\]|\\.)*"/g, '""')
  const lossy: string[] = []
  for (const m of withoutStrings.matchAll(/-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?/g)) {
    const lit = m[0]
    const n = Number(lit)
    if (!Number.isFinite(n)) {
      lossy.push(lit)
      continue
    }
    const intPart = lit.match(/^-?(\d+)(?:\.\d+)?$/)?.[1]
    if (intPart !== undefined && !Number.isSafeInteger(Number(intPart))) lossy.push(lit)
  }
  return lossy
}

// Indent of the first indented line, so a save keeps the file's style.
export function detectIndent(raw: string): string {
  const m = raw.match(/\n([ \t]+)\S/)
  return m?.[1] ?? '  '
}

export function serializeConfig(value: JsonValue, raw: string): string {
  const eol = raw.includes('\r\n') ? '\r\n' : '\n'
  let out = JSON.stringify(value, null, detectIndent(raw))
  if (eol === '\r\n') out = out.replace(/\n/g, '\r\n')
  return raw.endsWith('\n') ? out + eol : out
}

export type ParseResult =
  | { ok: true, value: JsonValue, formReason?: string }
  | { ok: false, error: string }

// Parses a config file. formReason is set when the form must not be used
// (the raw editor still works).
export function parseConfig(raw: string): ParseResult {
  let value: JsonValue
  try {
    value = JSON.parse(raw) as JsonValue
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : 'Invalid JSON' }
  }
  const lossy = findLossyNumbers(raw)
  if (lossy.length) {
    return { ok: true, value, formReason: `Contains numbers too large for the form (${lossy[0]}). Edit it as raw JSON.` }
  }
  if (!isPlainObject(value) && !Array.isArray(value)) {
    return { ok: true, value, formReason: 'The file is not a JSON object or list.' }
  }
  return { ok: true, value }
}

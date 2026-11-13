// Servers store either a concrete build ("0.6.8") or a patchline
// ("release", "pre-release"). Only builds get a "v" prefix; patchlines are
// shown as words instead of "vrelease".
export function formatHytaleVersion(version: string | null | undefined): string {
  const v = (version ?? '').trim()
  if (!v || v === 'latest') return 'Release'
  if (/^\d/.test(v)) return `v${v}`
  if (v === 'release') return 'Release'
  if (v === 'pre-release' || v === 'prerelease') return 'Pre-release'
  return v
}

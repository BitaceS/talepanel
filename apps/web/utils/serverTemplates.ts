import type { ServerTemplateSettings } from '~/types'

// Short human-readable chips for a template's settings, e.g.
// ["3 GB RAM", "2 cores", "Auto restart"]. Used on the picker cards and in
// the admin list. Pass tr (e.g. i18n's t) to get translated chips; the keys
// are under servers.templateSummary. Without it the chips are English.
export function templateSummary(
  s: ServerTemplateSettings,
  tr?: (key: string, n: number) => string,
): string[] {
  const out: string[] = []
  const l = (key: string, n: number, en: string) => (tr ? tr(`servers.templateSummary.${key}`, n) : en)
  if (s.ram_limit_mb) {
    out.push(s.ram_limit_mb % 1024 === 0 ? l('ramGb', s.ram_limit_mb / 1024, `${s.ram_limit_mb / 1024} GB RAM`) : l('ramMb', s.ram_limit_mb, `${s.ram_limit_mb} MB RAM`))
  }
  if (s.cpu_limit) {
    const cores = s.cpu_limit / 100
    out.push(Number.isInteger(cores) ? l('cores', cores, `${cores} ${cores === 1 ? 'core' : 'cores'}`) : l('cpuPercent', s.cpu_limit, `${s.cpu_limit}% CPU`))
  }
  if (s.disk_limit_mb) {
    out.push(s.disk_limit_mb % 1024 === 0 ? l('diskGb', s.disk_limit_mb / 1024, `${s.disk_limit_mb / 1024} GB disk`) : l('diskMb', s.disk_limit_mb, `${s.disk_limit_mb} MB disk`))
  }
  if (s.hytale_version === 'pre-release') out.push(l('preRelease', 0, 'Pre-release'))
  if (s.auto_restart) out.push(l('autoRestart', 0, 'Auto restart'))
  if (s.network_role === 'hub') out.push(l('networkHub', 0, 'Network hub'))
  if (s.network_role === 'gamemode') out.push(l('networkGamemode', 0, 'Network gamemode'))
  for (const f of s.launch_flags ?? []) out.push(f)
  return out
}

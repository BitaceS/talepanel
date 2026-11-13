import type { ServerTemplateSettings } from '~/types'

// Short human-readable chips for a template's settings, e.g.
// ["3 GB RAM", "2 cores", "Auto restart"]. Used on the picker cards and in
// the admin list.
export function templateSummary(s: ServerTemplateSettings): string[] {
  const out: string[] = []
  if (s.ram_limit_mb) {
    out.push(s.ram_limit_mb % 1024 === 0 ? `${s.ram_limit_mb / 1024} GB RAM` : `${s.ram_limit_mb} MB RAM`)
  }
  if (s.cpu_limit) {
    const cores = s.cpu_limit / 100
    out.push(Number.isInteger(cores) ? `${cores} ${cores === 1 ? 'core' : 'cores'}` : `${s.cpu_limit}% CPU`)
  }
  if (s.disk_limit_mb) {
    out.push(s.disk_limit_mb % 1024 === 0 ? `${s.disk_limit_mb / 1024} GB disk` : `${s.disk_limit_mb} MB disk`)
  }
  if (s.hytale_version === 'pre-release') out.push('Pre-release')
  if (s.auto_restart) out.push('Auto restart')
  if (s.network_role === 'hub') out.push('Network hub')
  if (s.network_role === 'gamemode') out.push('Network gamemode')
  for (const f of s.launch_flags ?? []) out.push(f)
  return out
}

import { defineStore } from 'pinia'

export interface ModuleConfig {
  id: string
  label: string
  description: string
  enabled: boolean
  hosterOnly: boolean
}

export type DeploymentProfile = 'solo' | 'hoster'

// DEFAULT_MODULES holds the static metadata.  All modules are visible by
// default; operators can disable any of them in Settings → Modules.  The
// `hosterOnly` flag is informational only (renders a "Hoster" badge).
const DEFAULT_MODULES: ModuleConfig[] = [
  { id: 'worlds', label: 'Worlds', description: 'Upload, download, and switch Hytale worlds', enabled: true, hosterOnly: false },
  { id: 'mods', label: 'Mods', description: 'CurseForge mod browser and installer', enabled: true, hosterOnly: false },
  { id: 'players', label: 'Players', description: 'Player management, bans, and whitelist', enabled: true, hosterOnly: false },
  { id: 'backups', label: 'Backups', description: 'On-demand and scheduled backup management', enabled: true, hosterOnly: false },
  { id: 'alerts', label: 'Alerts', description: 'Alert rules, notifications, and event monitoring', enabled: true, hosterOnly: false },
  { id: 'monitoring', label: 'Monitoring', description: 'Real-time metrics and DDoS monitoring', enabled: true, hosterOnly: true },
  { id: 'nodes', label: 'Nodes', description: 'Multi-node infrastructure management', enabled: true, hosterOnly: true },
]

// The deployment profile is now a quick "bulk preset" rather than a
// visibility gate.  "solo" disables hoster-flagged modules, "hoster"
// enables everything.  Users can still flip individual modules.
function defaultsFor(profile: DeploymentProfile): ModuleConfig[] {
  return DEFAULT_MODULES.map(m => ({
    ...m,
    enabled: profile === 'solo' && m.hosterOnly ? false : true,
  }))
}

const STORAGE_KEY = 'tp_modules'
const PROFILE_KEY = 'tp_deployment_profile'

export const useModulesStore = defineStore('modules', () => {
  const modules = ref<ModuleConfig[]>([])
  const profile = ref<DeploymentProfile>('solo')

  // applyProfile is for explicit user actions (e.g. toggling the profile
  // button in Settings).  It always re-seeds modules to the new profile's
  // defaults and persists both.
  function applyProfile(p: DeploymentProfile) {
    profile.value = p
    modules.value = defaultsFor(p)
    if (typeof localStorage !== 'undefined') {
      localStorage.setItem(PROFILE_KEY, p)
      localStorage.setItem(STORAGE_KEY, JSON.stringify(modules.value))
    }
  }

  // seedFromBackend is for boot-time hydration from /health/config.
  // It only records the server-reported profile — first-time module
  // defaults are always all-enabled (operators can disable in Settings).
  function seedFromBackend(p: DeploymentProfile) {
    const hadProfile = typeof localStorage !== 'undefined' && !!localStorage.getItem(PROFILE_KEY)
    profile.value = p
    if (typeof localStorage !== 'undefined' && !hadProfile) {
      localStorage.setItem(PROFILE_KEY, p)
    }
  }

  function load() {
    const storedProfile = (typeof localStorage !== 'undefined'
      ? (localStorage.getItem(PROFILE_KEY) as DeploymentProfile | null)
      : null)
    if (storedProfile === 'solo' || storedProfile === 'hoster') {
      profile.value = storedProfile
    }
    const seed = DEFAULT_MODULES.map(m => ({ ...m }))

    const stored = typeof localStorage !== 'undefined' ? localStorage.getItem(STORAGE_KEY) : null
    if (stored) {
      try {
        const parsed = JSON.parse(stored) as ModuleConfig[]
        modules.value = seed.map(def => {
          const saved = parsed.find(p => p.id === def.id)
          return saved ? { ...def, enabled: saved.enabled } : { ...def }
        })
      } catch {
        modules.value = seed
      }
    } else {
      modules.value = seed
    }
  }

  function save() {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(modules.value))
  }

  function toggle(moduleId: string, enabled: boolean) {
    const mod = modules.value.find(m => m.id === moduleId)
    if (mod) {
      mod.enabled = enabled
      save()
    }
  }

  function isEnabled(moduleId: string): boolean {
    const mod = modules.value.find(m => m.id === moduleId)
    return mod?.enabled ?? true
  }

  // Initialize on first use
  load()

  return { modules, profile, load, save, toggle, isEnabled, applyProfile, seedFromBackend }
})

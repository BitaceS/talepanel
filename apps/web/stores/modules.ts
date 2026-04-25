import { defineStore } from 'pinia'

export interface ModuleConfig {
  id: string
  label: string
  description: string
  enabled: boolean
  hosterOnly: boolean
}

export type DeploymentProfile = 'solo' | 'hoster'

// DEFAULT_MODULES holds the static metadata.  The `enabled` field is the
// "solo" default — fields marked hosterOnly are flipped to true when the
// installer/back-end reports a "hoster" deployment profile.
const DEFAULT_MODULES: ModuleConfig[] = [
  { id: 'worlds', label: 'Worlds', description: 'Upload, download, and switch Hytale worlds', enabled: true, hosterOnly: false },
  { id: 'mods', label: 'Mods', description: 'CurseForge mod browser and installer', enabled: true, hosterOnly: false },
  { id: 'players', label: 'Players', description: 'Player management, bans, and whitelist', enabled: true, hosterOnly: false },
  { id: 'backups', label: 'Backups', description: 'On-demand and scheduled backup management', enabled: true, hosterOnly: false },
  { id: 'alerts', label: 'Alerts', description: 'Alert rules, notifications, and event monitoring', enabled: true, hosterOnly: false },
  { id: 'monitoring', label: 'Monitoring', description: 'Real-time metrics and DDoS monitoring', enabled: false, hosterOnly: true },
  { id: 'nodes', label: 'Nodes', description: 'Multi-node infrastructure management', enabled: false, hosterOnly: true },
]

function defaultsFor(profile: DeploymentProfile): ModuleConfig[] {
  return DEFAULT_MODULES.map(m => ({
    ...m,
    enabled: profile === 'hoster' ? true : m.enabled,
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
  // It records the server-reported profile but only seeds module defaults
  // when no user choices have been saved yet.
  function seedFromBackend(p: DeploymentProfile) {
    const hadProfile = typeof localStorage !== 'undefined' && !!localStorage.getItem(PROFILE_KEY)
    profile.value = p
    if (typeof localStorage !== 'undefined' && !hadProfile) {
      localStorage.setItem(PROFILE_KEY, p)
    }
    if (typeof localStorage !== 'undefined' && !localStorage.getItem(STORAGE_KEY)) {
      modules.value = defaultsFor(p)
    }
  }

  function load() {
    const storedProfile = (typeof localStorage !== 'undefined'
      ? (localStorage.getItem(PROFILE_KEY) as DeploymentProfile | null)
      : null)
    if (storedProfile === 'solo' || storedProfile === 'hoster') {
      profile.value = storedProfile
    }
    const seed = defaultsFor(profile.value)

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

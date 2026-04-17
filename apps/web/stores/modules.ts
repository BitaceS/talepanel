import { defineStore } from 'pinia'

export interface ModuleConfig {
  id: string
  label: string
  description: string
  enabled: boolean
  hosterOnly: boolean
}

const DEFAULT_MODULES: ModuleConfig[] = [
  { id: 'worlds', label: 'Worlds', description: 'Upload, download, and switch Hytale worlds', enabled: true, hosterOnly: false },
  { id: 'mods', label: 'Mods', description: 'CurseForge mod browser and installer', enabled: true, hosterOnly: false },
  { id: 'players', label: 'Players', description: 'Player management, bans, and whitelist', enabled: true, hosterOnly: false },
  { id: 'backups', label: 'Backups', description: 'On-demand and scheduled backup management', enabled: true, hosterOnly: false },
  { id: 'alerts', label: 'Alerts', description: 'Alert rules, notifications, and event monitoring', enabled: true, hosterOnly: false },
  { id: 'monitoring', label: 'Monitoring', description: 'Real-time metrics and DDoS monitoring', enabled: true, hosterOnly: true },
  { id: 'nodes', label: 'Nodes', description: 'Multi-node infrastructure management', enabled: true, hosterOnly: true },
]

const STORAGE_KEY = 'tp_modules'

export const useModulesStore = defineStore('modules', () => {
  const modules = ref<ModuleConfig[]>([])

  function load() {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored) {
      try {
        const parsed = JSON.parse(stored) as ModuleConfig[]
        // Merge with defaults to pick up new modules
        modules.value = DEFAULT_MODULES.map(def => {
          const saved = parsed.find(p => p.id === def.id)
          return saved ? { ...def, enabled: saved.enabled } : { ...def }
        })
      } catch {
        modules.value = DEFAULT_MODULES.map(m => ({ ...m }))
      }
    } else {
      modules.value = DEFAULT_MODULES.map(m => ({ ...m }))
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

  return { modules, load, save, toggle, isEnabled }
})

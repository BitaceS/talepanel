import { defineStore } from 'pinia'
import { useApi } from '~/composables/useApi'
import type {
  Backup,
  BackupSchedule,
  BackupDestination,
  BackupDestinationProvider,
  BackupDestinationSummary,
} from '~/types'

export interface SaveDestinationPayload {
  name: string
  provider: string
  base_path: string
  options: Record<string, string>
  // empty values keep the stored secret
  secrets: Record<string, string>
  keep_local: boolean
  retention_count: number | null
}

export const useBackupsStore = defineStore('backups', () => {
  const api = useApi()
  const backups = ref<Backup[]>([])
  const schedules = ref<BackupSchedule[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  // Off-site destinations: summaries for every user (picker), full objects
  // and the provider catalogue for admins.
  const destinationSummaries = ref<BackupDestinationSummary[]>([])
  const destinations = ref<BackupDestination[]>([])
  const providers = ref<BackupDestinationProvider[]>([])

  async function fetchBackups(serverId?: string, opts: { silent?: boolean } = {}) {
    if (!opts.silent) loading.value = true
    error.value = null
    try {
      const query = serverId ? { server_id: serverId } : undefined
      const data = await api.get<{ backups: Backup[] }>('/backups', query)
      backups.value = data.backups ?? []
    } catch (err: unknown) {
      const e = err as { data?: { error?: string }; message?: string }
      error.value = e.data?.error ?? e.message ?? 'Failed to fetch backups'
    } finally {
      loading.value = false
    }
  }

  async function createBackup(payload: { server_id: string; world_name?: string; type?: string; destination_id?: string | null }) {
    const data = await api.post<{ backup: Backup }>('/backups', payload)
    backups.value.unshift(data.backup)
    return data.backup
  }

  async function deleteBackup(backupId: string) {
    await api.delete(`/backups/${backupId}`)
    backups.value = backups.value.filter(b => b.id !== backupId)
  }

  async function restoreBackup(backupId: string) {
    return await api.post<{ backup: Backup; message: string }>(`/backups/${backupId}/restore`)
  }

  async function retryUpload(backupId: string) {
    const data = await api.post<{ backup: Backup }>(`/backups/${backupId}/upload`)
    const i = backups.value.findIndex(b => b.id === backupId)
    if (i >= 0) backups.value[i] = data.backup
    return data.backup
  }

  async function fetchSchedules(serverId: string) {
    try {
      const data = await api.get<{ schedules: BackupSchedule[] }>(`/servers/${serverId}/backup-schedules`)
      schedules.value = data.schedules ?? []
    } catch {
      // silent
    }
  }

  async function createSchedule(payload: { server_id: string; cron_expr: string; type?: string; retention_count?: number; retention_days?: number; destination_id?: string | null }) {
    const data = await api.post<{ schedule: BackupSchedule }>('/backup-schedules', payload)
    schedules.value.unshift(data.schedule)
    return data.schedule
  }

  async function toggleSchedule(scheduleId: string, enabled: boolean) {
    await api.patch(`/backup-schedules/${scheduleId}`, { enabled })
    const s = schedules.value.find(s => s.id === scheduleId)
    if (s) s.enabled = enabled
  }

  async function deleteSchedule(scheduleId: string) {
    await api.delete(`/backup-schedules/${scheduleId}`)
    schedules.value = schedules.value.filter(s => s.id !== scheduleId)
  }

  // ── Destinations ─────────────────────────────────────────────────────────

  async function fetchDestinationSummaries() {
    try {
      const data = await api.get<{ destinations: BackupDestinationSummary[] }>('/backup-destinations')
      destinationSummaries.value = data.destinations ?? []
    } catch {
      destinationSummaries.value = []
    }
  }

  async function fetchDestinations() {
    const [d, p] = await Promise.all([
      api.get<{ destinations: BackupDestination[] }>('/admin/backup-destinations'),
      api.get<{ providers: BackupDestinationProvider[] }>('/admin/backup-destinations/providers'),
    ])
    destinations.value = d.destinations ?? []
    providers.value = p.providers ?? []
  }

  async function saveDestination(id: string | null, payload: SaveDestinationPayload) {
    const data = id
      ? await api.put<{ destination: BackupDestination }>(`/admin/backup-destinations/${id}`, payload)
      : await api.post<{ destination: BackupDestination }>('/admin/backup-destinations', payload)
    await Promise.all([fetchDestinations(), fetchDestinationSummaries()])
    return data.destination
  }

  async function deleteDestination(id: string) {
    await api.delete(`/admin/backup-destinations/${id}`)
    destinations.value = destinations.value.filter(d => d.id !== id)
    destinationSummaries.value = destinationSummaries.value.filter(d => d.id !== id)
  }

  async function testDestination(id: string) {
    const res = await api.post<{ ok: boolean; error?: string }>(`/admin/backup-destinations/${id}/test`)
    // A test may have resolved the OneDrive drive; show it.
    if (res.ok) await fetchDestinations().catch(() => {})
    return res
  }

  return {
    backups, schedules, loading, error,
    destinationSummaries, destinations, providers,
    fetchBackups, createBackup, deleteBackup, restoreBackup, retryUpload,
    fetchSchedules, createSchedule, toggleSchedule, deleteSchedule,
    fetchDestinationSummaries, fetchDestinations, saveDestination, deleteDestination, testDestination,
  }
})

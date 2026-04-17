import { defineStore } from 'pinia'
import { useApi } from '~/composables/useApi'
import type { Backup, BackupSchedule } from '~/types'

export const useBackupsStore = defineStore('backups', () => {
  const api = useApi()
  const backups = ref<Backup[]>([])
  const schedules = ref<BackupSchedule[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchBackups(serverId?: string) {
    loading.value = true
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

  async function createBackup(payload: { server_id: string; world_name?: string; type?: string; storage?: string }) {
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

  async function fetchSchedules(serverId: string) {
    try {
      const data = await api.get<{ schedules: BackupSchedule[] }>(`/servers/${serverId}/backup-schedules`)
      schedules.value = data.schedules ?? []
    } catch {
      // silent
    }
  }

  async function createSchedule(payload: { server_id: string; cron_expr: string; type?: string; storage?: string; retention_count?: number; retention_days?: number }) {
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

  return { backups, schedules, loading, error, fetchBackups, createBackup, deleteBackup, restoreBackup, fetchSchedules, createSchedule, toggleSchedule, deleteSchedule }
})

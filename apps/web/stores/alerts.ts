import { defineStore } from 'pinia'
import { useApi } from '~/composables/useApi'
import type { AlertRule, AlertEvent } from '~/types'

export const useAlertsStore = defineStore('alerts', () => {
  const api = useApi()
  const rules = ref<AlertRule[]>([])
  const events = ref<AlertEvent[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchRules() {
    loading.value = true
    error.value = null
    try {
      const data = await api.get<{ rules: AlertRule[] }>('/alerts/rules')
      rules.value = data.rules ?? []
    } catch (err: unknown) {
      const e = err as { data?: { error?: string }; message?: string }
      error.value = e.data?.error ?? e.message ?? 'Failed to fetch alert rules'
    } finally {
      loading.value = false
    }
  }

  async function createRule(payload: { server_id?: string; type: string; threshold?: number; channels?: string[] }) {
    const data = await api.post<{ rule: AlertRule }>('/alerts/rules', payload)
    rules.value.unshift(data.rule)
    return data.rule
  }

  async function toggleRule(ruleId: string, enabled: boolean) {
    await api.patch(`/alerts/rules/${ruleId}`, { enabled })
    const r = rules.value.find(r => r.id === ruleId)
    if (r) r.enabled = enabled
  }

  async function deleteRule(ruleId: string) {
    await api.delete(`/alerts/rules/${ruleId}`)
    rules.value = rules.value.filter(r => r.id !== ruleId)
  }

  async function fetchEvents() {
    try {
      const data = await api.get<{ events: AlertEvent[] }>('/alerts/events')
      events.value = data.events ?? []
    } catch {
      // silent
    }
  }

  async function resolveEvent(eventId: string) {
    await api.post(`/alerts/events/${eventId}/resolve`)
    const e = events.value.find(e => e.id === eventId)
    if (e) { e.resolved = true; e.resolved_at = new Date().toISOString() }
  }

  return { rules, events, loading, error, fetchRules, createRule, toggleRule, deleteRule, fetchEvents, resolveEvent }
})

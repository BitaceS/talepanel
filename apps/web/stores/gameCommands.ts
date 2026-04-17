import { defineStore } from 'pinia'
import { useApi } from '~/composables/useApi'

export interface CommandParam {
  name: string
  type: string
  required: boolean
  placeholder: string
  default?: string
}

export interface GameCommand {
  id: string
  server_id?: string
  category: string
  name: string
  description: string
  command_template: string
  icon: string
  params: CommandParam[]
  sort_order: number
  is_default: boolean
  min_role: string
  source: string
  source_plugin: string | null
}

interface GroupedCommands {
  [category: string]: GameCommand[]
}

export const useGameCommandsStore = defineStore('gameCommands', {
  state: () => ({
    commands: [] as GameCommand[],
    loading: false,
    executing: null as string | null, // command ID currently executing
    lastResult: null as { message: string; command: string } | null,
    error: null as string | null,
  }),

  getters: {
    grouped(): GroupedCommands {
      const groups: GroupedCommands = {}
      const cmds = Array.isArray(this.commands) ? this.commands : []
      for (const cmd of cmds) {
        if (!groups[cmd.category]) {
          groups[cmd.category] = []
        }
        groups[cmd.category].push(cmd)
      }
      return groups
    },

    groupedBySource(): Record<string, GameCommand[]> {
      const groups: Record<string, GameCommand[]> = {}
      const cmds = Array.isArray(this.commands) ? this.commands : []
      for (const cmd of cmds) {
        const source = cmd.source || 'built-in'
        if (!groups[source]) {
          groups[source] = []
        }
        groups[source].push(cmd)
      }
      return groups
    },

    categories(): string[] {
      return Object.keys(this.grouped)
    },

    sources(): string[] {
      return Object.keys(this.groupedBySource)
    },
  },

  actions: {
    async fetchCommands(serverId: string) {
      this.loading = true
      this.error = null
      try {
        const api = useApi()
        const data = await api.get<GameCommand[]>(`/servers/${serverId}/game-commands`)
        this.commands = Array.isArray(data) ? data : []
      } catch (e: any) {
        this.error = e?.message || 'Failed to load commands'
        this.commands = []
      } finally {
        this.loading = false
      }
    },

    async executeCommand(serverId: string, cmd: GameCommand, params: Record<string, string>) {
      this.executing = cmd.id
      this.error = null
      this.lastResult = null
      try {
        const api = useApi()
        const result = await api.post<{ message: string; command: string }>(
          `/servers/${serverId}/game-commands/execute`,
          {
            command_id: cmd.id,
            command_template: cmd.command_template,
            params,
          }
        )
        this.lastResult = result
      } catch (e: any) {
        this.error = e?.message || 'Failed to execute command'
      } finally {
        this.executing = null
      }
    },

    async createCommand(serverId: string, cmd: Partial<GameCommand>) {
      this.error = null
      try {
        const api = useApi()
        await api.post(`/servers/${serverId}/game-commands`, cmd)
        await this.fetchCommands(serverId)
      } catch (e: any) {
        this.error = e?.message || 'Failed to create command'
      }
    },

    async deleteCommand(serverId: string, cmdId: string) {
      this.error = null
      try {
        const api = useApi()
        await api.delete(`/servers/${serverId}/game-commands/${cmdId}`)
        this.commands = this.commands.filter((c) => c.id !== cmdId)
      } catch (e: any) {
        this.error = e?.message || 'Failed to delete command'
      }
    },
  },
})

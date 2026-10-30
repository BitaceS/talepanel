<script setup lang="ts">
import type { Server } from '~/types'

// Server list for the game theme, laid out like a multiplayer server select.
const props = defineProps<{
  servers: Server[]
  loading: boolean
  pendingId: string | null
  error: string | null
}>()

const emit = defineEmits<{
  open: [server: Server]
  power: [server: Server]
  restart: [server: Server]
  remove: [server: Server]
  create: []
}>()

type Tab = 'all' | 'online' | 'stopped'
const tab = ref<Tab>('all')

const isOnline = (s: Server) => s.status === 'running' || s.status === 'starting'

const counts = computed(() => ({
  all: props.servers.length,
  online: props.servers.filter(isOnline).length,
  stopped: props.servers.filter(s => !isOnline(s)).length,
}))

const visible = computed(() => {
  if (tab.value === 'online') return props.servers.filter(isOnline)
  if (tab.value === 'stopped') return props.servers.filter(s => !isOnline(s))
  return props.servers
})

const selectedId = ref<string | null>(null)
const selected = computed(() => visible.value.find(s => s.id === selectedId.value) ?? visible.value[0] ?? null)

watch(visible, (list) => {
  if (!list.some(s => s.id === selectedId.value)) selectedId.value = list[0]?.id ?? null
}, { immediate: true })

const nodeCount = computed(() => new Set(props.servers.map(s => s.node_id)).size)

function tone(s: Server): 'gold' | 'green' | 'stone' | 'red' {
  if (s.status === 'crashed') return 'red'
  if (s.status === 'running') return 'gold'
  if (s.status === 'starting' || s.status === 'stopping' || s.status === 'installing') return 'green'
  return 'stone'
}

function statusText(s: Server): string {
  switch (s.status) {
    case 'running': return 'Online'
    case 'starting': return 'Starting'
    case 'stopping': return 'Stopping'
    case 'installing': return 'Installing'
    case 'crashed': return 'Crashed'
    default: return 'Stopped'
  }
}

const busy = (s: Server | null) => !s || props.pendingId === s.id || ['starting', 'stopping', 'installing'].includes(s.status)

function move(delta: number) {
  const list = visible.value
  if (!list.length) return
  const i = Math.max(0, list.findIndex(s => s.id === selected.value?.id))
  selectedId.value = list[(i + delta + list.length) % list.length].id
}

function onKey(e: KeyboardEvent) {
  const target = e.target as HTMLElement | null
  if (target && ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName)) return
  if (e.key === 'ArrowDown') { e.preventDefault(); move(1) }
  else if (e.key === 'ArrowUp') { e.preventDefault(); move(-1) }
  else if (e.key === 'Enter' && selected.value) emit('open', selected.value)
}

onMounted(() => window.addEventListener('keydown', onKey))
onUnmounted(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <div class="server-select">
    <h1 class="title">Server Select</h1>
    <p class="sub">
      {{ counts.all }} {{ counts.all === 1 ? 'server' : 'servers' }} on {{ nodeCount }} {{ nodeCount === 1 ? 'node' : 'nodes' }}
      · {{ counts.online }} online
    </p>

    <div v-if="error" class="error">{{ error }}</div>

    <div class="frame">
      <span class="studs" />
      <div class="tabs" role="tablist">
        <button
          v-for="t in (['all', 'online', 'stopped'] as const)"
          :key="t"
          role="tab"
          :aria-selected="tab === t"
          :class="['tab', { on: tab === t }]"
          @click="tab = t"
        >
          {{ t === 'all' ? 'All' : t === 'online' ? 'Online' : 'Stopped' }}<span class="n">{{ counts[t] }}</span>
        </button>
      </div>

      <div v-if="loading && !servers.length" class="empty">Loading servers...</div>
      <div v-else-if="!visible.length" class="empty">
        <template v-if="tab === 'all'">No servers yet. Create your first one below.</template>
        <template v-else>No {{ tab }} servers.</template>
      </div>
      <div v-else class="list" role="listbox" aria-label="Servers">
        <div
          v-for="s in visible"
          :key="s.id"
          role="option"
          :aria-selected="selected?.id === s.id"
          :class="['row', { sel: selected?.id === s.id, off: tone(s) === 'stone' }]"
          @click="selectedId = s.id"
          @dblclick="emit('open', s)"
        >
          <GameBlockIcon :tone="tone(s)" />
          <div class="min-w-0">
            <div class="name">{{ s.name }}</div>
            <div class="motd">
              <span :class="s.status === 'running' ? 'ok' : s.status === 'crashed' ? 'bad' : ''">{{ statusText(s) }}</span>
              · {{ s.node_fqdn ?? 'node' }}:{{ s.port }}
              · Hytale {{ s.hytale_version }}
              <template v-if="s.active_world"> · World {{ s.active_world }}</template>
            </div>
          </div>
          <div class="meta">
            <div class="ram">{{ s.ram_limit_mb ? `${s.ram_limit_mb} MB` : 'no RAM limit' }}</div>
            <span :class="['sig', { off: s.status !== 'running' }]" :title="statusText(s)">
              <i style="height:6px" /><i style="height:11px" /><i style="height:16px" />
            </span>
          </div>
        </div>
      </div>
    </div>

    <div class="bar">
      <button class="gbtn gold" :disabled="!selected" @click="selected && emit('open', selected)">Open Server</button>
      <button class="gbtn" :disabled="busy(selected)" @click="selected && emit('power', selected)">
        {{ selected?.status === 'running' ? 'Stop' : 'Start' }}
      </button>
      <button class="gbtn" :disabled="busy(selected) || selected?.status !== 'running'" @click="selected && emit('restart', selected)">Restart</button>
      <button class="gbtn" @click="emit('create')">New Server</button>
      <button class="gbtn danger" :disabled="!selected || pendingId === selected.id" @click="selected && emit('remove', selected)">Delete</button>
    </div>
    <p class="hint"><kbd>↑</kbd> <kbd>↓</kbd> select · <kbd>Enter</kbd> open · double-click opens</p>
  </div>
</template>

<style scoped>
.server-select {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 28px 16px 40px;
  min-height: 100%;
}
.title {
  margin: 0 0 4px;
  font: 800 44px/1 'Alegreya SC', Georgia, serif;
  letter-spacing: 0.02em;
  color: #efe3c8;
  text-shadow: 3px 3px 0 #000;
}
.sub {
  margin: 0 0 16px;
  font-size: 15px;
  color: #efe3c8;
  text-shadow: 1px 1px 0 #000;
}
.error {
  width: min(760px, 100%);
  margin-bottom: 12px;
  padding: 10px 14px;
  background: #4a1d17;
  color: #f0a090;
  box-shadow: 0 0 0 2px #000;
}

/* Stone frame: hard bevel, black outline, gold corner studs */
.frame {
  position: relative;
  width: min(760px, 100%);
  background: rgba(16, 12, 20, 0.9);
  box-shadow:
    0 0 0 2px #000,
    inset 0 0 0 2px #4b3d38,
    inset 3px 3px 0 2px #6a5646,
    inset -3px -3px 0 2px #120d10,
    0 14px 0 -4px rgba(0, 0, 0, 0.45);
}
.frame::before,
.frame::after,
.studs::before,
.studs::after {
  content: '';
  position: absolute;
  z-index: 1;
  width: 8px;
  height: 8px;
  background: #a8771f;
  box-shadow: inset -2px -2px 0 #5e4210, inset 2px 2px 0 #d9a646;
  outline: 2px solid #000;
}
.frame::before { top: -5px; left: -5px; }
.frame::after { top: -5px; right: -5px; }
.studs::before { bottom: -5px; left: -5px; }
.studs::after { bottom: -5px; right: -5px; }

.tabs { display: flex; border-bottom: 2px solid #000; }
.tab {
  padding: 10px 18px;
  font: 700 15px/1 'Alegreya SC', Georgia, serif;
  color: #c9bba5;
  background: #1a141d;
  border-right: 2px solid #000;
  box-shadow: inset 0 -3px 0 rgba(0, 0, 0, 0.35);
}
.tab:hover { color: #efe3c8; }
.tab.on { color: #2b1e12; background: #f2c14e; box-shadow: inset 0 -3px 0 #a8771f; }
.tab .n { margin-left: 6px; font: 500 12px 'JetBrains Mono', monospace; opacity: 0.8; }

.empty { padding: 28px 20px; text-align: center; color: #c9bba5; }
.list { max-height: 52vh; overflow: auto; }
.row {
  position: relative;
  display: grid;
  grid-template-columns: 52px 1fr auto;
  gap: 14px;
  align-items: center;
  padding: 12px 16px 12px 24px;
  border-bottom: 2px solid #0a080d;
  background: linear-gradient(#1b1520, #16111a);
  cursor: pointer;
  user-select: none;
}
.row:hover { background: linear-gradient(#241c29, #1d1622); }
.row.sel { background: linear-gradient(#2c2117, #221911); box-shadow: inset 0 0 0 2px #f2c14e, inset 0 0 0 4px #000; }
.row.sel::before {
  content: '';
  position: absolute;
  left: 9px;
  top: 50%;
  width: 8px;
  height: 8px;
  margin-top: -4px;
  background: #f2c14e;
  transform: rotate(45deg);
  outline: 2px solid #000;
}
.row.off .name, .row.off .motd { color: #bdb1a3; }
.name {
  font: 700 20px/1.1 'Alegreya Sans', sans-serif;
  color: #fff6e2;
  text-shadow: 1px 1px 0 #000;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.motd {
  margin-top: 4px;
  font-size: 14px;
  color: #c9bba5;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.motd .ok { color: #86c160; }
.motd .bad { color: #f0a090; }
.meta { text-align: right; }
.ram { font: 500 13px 'JetBrains Mono', monospace; color: #efe3c8; }
.sig { display: inline-flex; gap: 2px; align-items: flex-end; height: 16px; margin-top: 6px; }
.sig i { display: block; width: 5px; background: #86c160; outline: 1px solid #000; }
.sig.off i { background: #3f3946; }

.bar { display: flex; flex-wrap: wrap; gap: 10px; justify-content: center; margin-top: 18px; width: min(760px, 100%); }
.gbtn {
  font: 700 15px/1 'Alegreya SC', Georgia, serif;
  letter-spacing: 0.03em;
  color: #efe3c8;
  background: #2e2530;
  padding: 12px 20px;
  box-shadow: 0 0 0 2px #000, inset 2px 2px 0 #4f4250, inset -2px -4px 0 #17111a;
}
.gbtn:hover:not(:disabled) { background: #3a2f3c; }
.gbtn:active:not(:disabled) { transform: translateY(2px); box-shadow: 0 0 0 2px #000, inset 2px 2px 0 #17111a; }
.gbtn:disabled { opacity: 0.45; cursor: not-allowed; }
.gbtn:focus-visible, .tab:focus-visible { outline: 2px solid #f2c14e; outline-offset: 2px; }
.gbtn.gold { color: #2b1e12; background: #f2c14e; box-shadow: 0 0 0 2px #000, inset 2px 2px 0 #ffe39a, inset -2px -4px 0 #a8771f; }
.gbtn.gold:hover:not(:disabled) { background: #f7cd65; }
.gbtn.danger { background: #4a1d17; box-shadow: 0 0 0 2px #000, inset 2px 2px 0 #7a3328, inset -2px -4px 0 #260c09; }
.hint { margin-top: 12px; font-size: 13px; color: #c9bba5; text-shadow: 1px 1px 0 #000; }
.hint kbd {
  padding: 2px 7px;
  font: 500 12px 'JetBrains Mono', monospace;
  color: #efe3c8;
  background: #2e2530;
  box-shadow: 0 0 0 1px #000;
}
</style>

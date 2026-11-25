<script setup lang="ts">
import { ChevronRight, Plus, X } from '@lucide/vue'
import {
  fieldInfo, humanizeKey, isPlainObject, isSecretKey,
  type JsonValue,
} from '~/utils/configForm'

// One value of a config file. Objects and lists render their children with
// this same component. Values are never mutated in place: every change emits
// a new value up to the parent, which rebuilds its own object.

const props = withDefaults(defineProps<{
  file: string
  path: string[]
  value: JsonValue
  depth?: number
}>(), { depth: 0 })

const emit = defineEmits<{ update: [value: JsonValue] }>()

const key = computed(() => props.path[props.path.length - 1] ?? '')
const info = computed(() => fieldInfo(props.file, props.path))
const label = computed(() => info.value.label ?? humanizeKey(key.value))
const inputId = computed(() => `cfg-${props.file}-${props.path.join('-')}`.replace(/[^\w-]/g, '_'))

const kind = computed(() => {
  const v = props.value
  if (v === null) return 'null'
  if (Array.isArray(v)) {
    if (!v.length) {
      const items = info.value.items
      return items === 'string' ? 'string-list' : items === 'number' ? 'number-list' : 'list'
    }
    return v.every(x => typeof x === 'string') ? 'string-list'
      : v.every(x => typeof x === 'number') ? 'number-list'
        : 'list'
  }
  if (isPlainObject(v)) return 'object'
  return typeof v as 'string' | 'number' | 'boolean'
})

const open = ref(props.depth < 1)
const showSecret = ref(false)

// Numbers keep their own text so "1." or "-" while typing doesn't jump.
const numberText = ref(typeof props.value === 'number' ? String(props.value) : '')
watch(() => props.value, (v) => {
  if (typeof v === 'number' && Number(numberText.value) !== v) numberText.value = String(v)
})
const numberInvalid = computed(() => kind.value === 'number' && (numberText.value.trim() === '' || !Number.isFinite(Number(numberText.value))))
function onNumber(text: string) {
  numberText.value = text
  const n = Number(text)
  if (text.trim() !== '' && Number.isFinite(n)) emit('update', n)
}

const selectOptions = computed(() => {
  const opts = info.value.options
  if (!opts || typeof props.value !== 'string') return null
  return opts.includes(props.value) ? opts : [props.value, ...opts]
})

function setChild(childKey: string, child: JsonValue) {
  if (!isPlainObject(props.value)) return
  emit('update', { ...props.value, [childKey]: child })
}

function setItem(index: number, item: JsonValue) {
  if (!Array.isArray(props.value)) return
  const next = [...props.value]
  next[index] = item
  emit('update', next)
}

function removeItem(index: number) {
  if (!Array.isArray(props.value)) return
  emit('update', props.value.filter((_, i) => i !== index))
}

const newItem = ref('')
function addItem() {
  if (!Array.isArray(props.value)) return
  const text = newItem.value.trim()
  if (!text) return
  if (kind.value === 'number-list') {
    const n = Number(text)
    if (!Number.isFinite(n)) return
    emit('update', [...props.value, n])
  } else {
    emit('update', [...props.value, text])
  }
  newItem.value = ''
}

const childCount = computed(() => {
  const v = props.value
  if (Array.isArray(v)) return `${v.length} ${v.length === 1 ? 'entry' : 'entries'}`
  if (isPlainObject(v)) return `${Object.keys(v).length} ${Object.keys(v).length === 1 ? 'setting' : 'settings'}`
  return ''
})

const inputClass = 'w-full bg-tp-surface-lowest border border-tp-border rounded-lg px-3 py-1.5 text-sm text-tp-text focus:outline-none focus:ring-2 focus:ring-tp-primary/40'
</script>

<template>
  <!-- Object / mixed list: collapsible group -->
  <div v-if="kind === 'object' || kind === 'list'" :class="depth > 0 ? 'border-l border-tp-border pl-3' : ''">
    <button
      v-if="depth > 0"
      type="button"
      class="flex items-center gap-1.5 py-1.5 text-sm font-medium text-tp-text hover:text-tp-primary"
      :aria-expanded="open"
      @click="open = !open"
    >
      <ChevronRight :class="['w-3.5 h-3.5 transition-transform', open ? 'rotate-90' : '']" />
      {{ label }}
      <span class="text-tp-outline text-xs font-normal">{{ childCount }}</span>
    </button>
    <div v-if="open || depth === 0" class="space-y-3" :class="depth > 0 ? 'pb-2 pt-1' : ''">
      <template v-if="kind === 'object'">
        <ServerConfigField
          v-for="(child, childKey) in (value as Record<string, JsonValue>)"
          :key="childKey"
          :file="file"
          :path="[...path, String(childKey)]"
          :value="child"
          :depth="depth + 1"
          @update="v => setChild(String(childKey), v)"
        />
        <p v-if="!Object.keys(value as object).length" class="text-tp-outline text-xs">Empty.</p>
      </template>
      <template v-else>
        <div v-for="(item, i) in (value as JsonValue[])" :key="i" class="flex items-start gap-2">
          <div class="flex-1 min-w-0">
            <ServerConfigField
              :file="file"
              :path="[...path, String(i)]"
              :value="item"
              :depth="depth + 1"
              @update="v => setItem(i, v)"
            />
          </div>
          <button type="button" class="mt-1.5 p-1 text-tp-outline hover:text-tp-danger" :aria-label="`Remove entry ${i + 1}`" @click="removeItem(i)">
            <X class="w-3.5 h-3.5" />
          </button>
        </div>
        <p v-if="!(value as JsonValue[]).length" class="text-tp-outline text-xs">No entries. Add new ones in Raw JSON.</p>
      </template>
    </div>
  </div>

  <!-- Boolean -->
  <label v-else-if="kind === 'boolean'" :for="inputId" class="flex items-center justify-between gap-4 py-1">
    <span class="min-w-0">
      <span class="block text-sm text-tp-text">{{ label }}</span>
      <span v-if="info.hint" class="block text-xs text-tp-outline">{{ info.hint }}</span>
    </span>
    <input
      :id="inputId"
      type="checkbox"
      role="switch"
      class="h-4 w-4 accent-tp-primary shrink-0"
      :checked="value as boolean"
      @change="emit('update', ($event.target as HTMLInputElement).checked)"
    >
  </label>

  <!-- Scalars and plain lists -->
  <div v-else class="space-y-1">
    <label :for="inputId" class="block text-xs font-medium text-tp-muted">
      {{ label }}
      <span v-if="label !== key && !/^\d+$/.test(key)" class="text-tp-outline font-mono font-normal ml-1">{{ key }}</span>
    </label>

    <select
      v-if="kind === 'string' && selectOptions"
      :id="inputId"
      :class="inputClass"
      :value="value as string"
      @change="emit('update', ($event.target as HTMLSelectElement).value)"
    >
      <option v-for="o in selectOptions" :key="o" :value="o">{{ o }}</option>
    </select>

    <div v-else-if="kind === 'string'" class="flex gap-2">
      <input
        :id="inputId"
        :type="isSecretKey(key) && !showSecret ? 'password' : 'text'"
        :class="inputClass"
        :value="value as string"
        autocomplete="off"
        spellcheck="false"
        @input="emit('update', ($event.target as HTMLInputElement).value)"
      >
      <button
        v-if="isSecretKey(key)"
        type="button"
        class="text-xs text-tp-muted hover:text-tp-text px-2"
        @click="showSecret = !showSecret"
      >
        {{ showSecret ? 'Hide' : 'Show' }}
      </button>
    </div>

    <template v-else-if="kind === 'number'">
      <input
        :id="inputId"
        type="text"
        inputmode="decimal"
        :class="[inputClass, numberInvalid ? 'border-tp-danger' : '']"
        :value="numberText"
        :aria-invalid="numberInvalid"
        @input="onNumber(($event.target as HTMLInputElement).value)"
      >
      <p v-if="numberInvalid" class="text-xs text-tp-danger">Enter a number. The last valid value is kept.</p>
    </template>

    <div v-else-if="kind === 'string-list' || kind === 'number-list'" class="space-y-1.5">
      <div v-for="(item, i) in (value as (string | number)[])" :key="i" class="flex items-center gap-2">
        <input
          :class="inputClass"
          :value="item"
          :aria-label="`${label} ${i + 1}`"
          spellcheck="false"
          @input="setItem(i, kind === 'number-list' && Number.isFinite(Number(($event.target as HTMLInputElement).value)) ? Number(($event.target as HTMLInputElement).value) : ($event.target as HTMLInputElement).value)"
        >
        <button type="button" class="p-1 text-tp-outline hover:text-tp-danger" :aria-label="`Remove ${item}`" @click="removeItem(i)">
          <X class="w-3.5 h-3.5" />
        </button>
      </div>
      <div class="flex items-center gap-2">
        <input
          v-model="newItem"
          :class="inputClass"
          :placeholder="kind === 'number-list' ? 'Add a number' : 'Add an entry'"
          :aria-label="`Add to ${label}`"
          spellcheck="false"
          @keydown.enter.prevent="addItem"
        >
        <button type="button" class="p-1 text-tp-muted hover:text-tp-primary" :aria-label="`Add to ${label}`" @click="addItem">
          <Plus class="w-4 h-4" />
        </button>
      </div>
    </div>

    <p v-else class="text-xs text-tp-outline font-mono">null <span class="font-sans">(edit in raw JSON)</span></p>

    <p v-if="info.hint" class="text-xs text-tp-outline">{{ info.hint }}</p>
  </div>
</template>

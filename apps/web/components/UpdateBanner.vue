<script setup lang="ts">
const { info, dismissed } = useUpdateCheck()
</script>

<template>
  <div
    v-if="info?.has_update && !dismissed"
    class="flex items-center gap-3 px-6 py-2.5 bg-tp-warning/15 text-tp-warning text-sm border-b border-tp-warning/20"
  >
    <span class="material-symbols-outlined text-base shrink-0">system_update_alt</span>
    <span class="flex-1">
      TalePanel {{ info.latest_version }} is available — you're on {{ info.current_version }}.
    </span>
    <a
      :href="info.release_url"
      target="_blank"
      rel="noopener"
      class="font-semibold underline underline-offset-2 hover:text-tp-warning/80 transition-colors shrink-0"
    >View release</a>
    <button
      class="ml-1 w-6 h-6 flex items-center justify-center rounded hover:bg-tp-warning/20 transition-colors shrink-0"
      title="Dismiss"
      @click="dismissed = true"
    >
      <span class="material-symbols-outlined text-base">close</span>
    </button>
  </div>
</template>

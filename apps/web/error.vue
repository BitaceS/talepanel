<script setup lang="ts">
interface NuxtError {
  statusCode: number
  statusMessage?: string
  message?: string
}

const props = defineProps<{
  error: NuxtError
}>()

const errorMessages: Record<number, string> = {
  404: 'The page you are looking for does not exist.',
  403: 'You do not have permission to access this page.',
  500: 'Something went wrong on our end. Please try again later.',
  503: 'The service is temporarily unavailable. Please try again later.',
}

const message = computed(
  () =>
    errorMessages[props.error.statusCode] ??
    props.error.statusMessage ??
    props.error.message ??
    'An unexpected error occurred.'
)

const errorTitle = computed(() => {
  if (props.error.statusCode === 404) return 'Page Not Found'
  if (props.error.statusCode === 403) return 'Access Denied'
  if (props.error.statusCode === 500) return 'Server Error'
  return 'Something Went Wrong'
})

function handleError() {
  clearError({ redirect: '/' })
}
</script>

<template>
  <div class="min-h-screen bg-tp-bg flex flex-col items-center justify-center px-4 relative overflow-hidden">
    <!-- Background decoration -->
    <div class="absolute inset-0 pointer-events-none">
      <div class="absolute -top-40 -right-40 w-[500px] h-[500px] bg-tp-primary/5 rounded-full blur-[120px]" />
      <div class="absolute -bottom-40 -left-40 w-[400px] h-[400px] bg-tp-danger/5 rounded-full blur-[100px]" />
    </div>

    <div class="relative z-10 text-center max-w-md w-full">
      <!-- Icon -->
      <div class="w-16 h-16 bg-tp-surface2 rounded-2xl flex items-center justify-center mx-auto mb-6">
        <span class="material-symbols-outlined text-tp-warning text-3xl">warning</span>
      </div>

      <!-- Error code -->
      <div class="text-8xl font-display font-bold text-tp-accent/15 leading-none mb-4 select-none">
        {{ error.statusCode }}
      </div>

      <!-- Message -->
      <h1 class="text-tp-text font-display font-bold text-xl mb-2">{{ errorTitle }}</h1>
      <p class="text-tp-muted text-sm mb-8">{{ message }}</p>

      <!-- Actions -->
      <button
        class="inline-flex items-center gap-2 bg-tp-primary text-tp-on-primary text-sm font-semibold px-5 py-2.5 rounded-xl hover:brightness-110 transition-all"
        @click="handleError"
      >
        <span class="material-symbols-outlined text-base">arrow_back</span>
        Back to Dashboard
      </button>

      <!-- Branding -->
      <div class="mt-12 flex items-center justify-center gap-2">
        <div class="w-6 h-6 bg-tp-primary rounded-lg flex items-center justify-center">
          <span class="material-symbols-outlined text-tp-on-primary text-sm">cloud_queue</span>
        </div>
        <span class="text-tp-outline text-sm">TalePanel &middot; by Tyraxo</span>
      </div>
    </div>
  </div>
</template>

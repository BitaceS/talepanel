<template>
  <div class="min-h-screen bg-tp-bg flex flex-col items-center justify-center px-4 relative overflow-hidden">
    <!-- Background decoration -->
    <div class="absolute inset-0 pointer-events-none">
      <div class="absolute -top-40 -right-40 w-[500px] h-[500px] bg-tp-primary/5 rounded-full blur-[120px]" />
      <div class="absolute -bottom-40 -left-40 w-[400px] h-[400px] bg-tp-tertiary/5 rounded-full blur-[100px]" />
      <div class="absolute top-1/3 left-1/2 -translate-x-1/2 w-[600px] h-[300px] bg-tp-accent/3 rounded-full blur-[150px]" />
    </div>

    <div class="w-full max-w-md relative z-10">
      <!-- Card (glass effect) -->
      <div class="bg-tp-surface/90 backdrop-blur-xl rounded-2xl p-8 shadow-ambient">
        <!-- Logo -->
        <div class="flex flex-col items-center mb-8">
          <div class="flex items-center gap-3 mb-3">
            <div class="w-10 h-10 bg-tp-primary rounded-xl flex items-center justify-center">
              <span class="material-symbols-outlined text-tp-on-primary text-xl">cloud_queue</span>
            </div>
            <span class="text-tp-text font-display font-bold text-2xl tracking-tight">TalePanel</span>
          </div>
          <span class="text-tp-muted text-xs uppercase tracking-widest font-medium">The Ethereal Command</span>
        </div>

        <!-- Page content -->
        <slot />
      </div>

      <!-- Footer -->
      <p class="text-center text-tp-outline text-xs mt-6">
        TalePanel &middot; by Tyraxo
      </p>
    </div>
  </div>
</template>

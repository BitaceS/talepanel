<template>
  <div class="min-h-screen bg-tp-bg flex flex-col items-center justify-center px-4 relative overflow-hidden">
    <!-- Background: faint block grid, like the voxel mark -->
    <div class="absolute inset-0 pointer-events-none auth-grid" />

    <div class="w-full max-w-md relative z-10">
      <!-- Card -->
      <div class="bg-tp-surface border border-tp-border border-t-2 border-t-tp-primary rounded-2xl p-8 shadow-ambient">
        <!-- Logo -->
        <div class="flex flex-col items-center mb-8">
          <div class="flex items-center gap-3 mb-3">
            <img src="/logo.svg" alt="" class="w-11 h-11" />
            <span class="text-tp-text font-display font-bold text-3xl">TalePanel</span>
          </div>
          <span class="text-tp-outline font-mono text-[11px] uppercase tracking-[0.18em]">Hytale server panel</span>
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

<style scoped>
.auth-grid {
  background-image:
    linear-gradient(rgba(242, 193, 78, 0.035) 1px, transparent 1px),
    linear-gradient(90deg, rgba(242, 193, 78, 0.035) 1px, transparent 1px);
  background-size: 40px 40px;
  mask-image: radial-gradient(ellipse at center, #000 20%, transparent 75%);
}
</style>

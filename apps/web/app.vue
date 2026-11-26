<script setup lang="ts">
// Keep <html lang> in step with the UI language (screen readers, hyphenation).
const { locale } = useI18n()
useHead({ htmlAttrs: { lang: locale } })
</script>

<template>
  <NuxtLayout>
    <NuxtPage />
  </NuxtLayout>
</template>

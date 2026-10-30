<script setup lang="ts">
// 16x16 pixel-art block used as a server icon in the game theme.
const props = defineProps<{ tone: 'gold' | 'green' | 'stone' | 'red' }>()

const PALETTES: Record<string, string[]> = {
  gold: ['#ffe39a', '#f2c14e', '#e8863b', '#a9531a'],
  green: ['#b6e08a', '#86c160', '#4f7a37', '#35552a'],
  stone: ['#8f8a96', '#6b6570', '#4a444f', '#35303a'],
  red: ['#f0a090', '#d0533f', '#9b2f24', '#5e1a14'],
}

const canvas = ref<HTMLCanvasElement | null>(null)

function draw() {
  const g = canvas.value?.getContext('2d')
  if (!g) return
  const p = PALETTES[props.tone]
  g.clearRect(0, 0, 16, 16)
  for (let y = 0; y < 16; y++) {
    for (let x = 0; x < 16; x++) {
      let col: string | null = null
      if (y >= 1 && y <= 8 && Math.abs(x - 7.5) + Math.abs(y - 4.5) * 2 <= 8) col = (x + y) % 5 === 0 ? p[0] : p[1]
      else if (y >= 5 && x <= 7 && y <= 15 - Math.floor((7 - x) / 2)) col = (y * 3 + x) % 7 === 0 ? p[1] : p[2]
      else if (y >= 5 && x >= 8 && y <= 15 - Math.floor((x - 8) / 2)) col = (y + x * 2) % 6 === 0 ? p[2] : p[3]
      if (col) {
        g.fillStyle = col
        g.fillRect(x, y, 1, 1)
      }
    }
  }
}

onMounted(draw)
watch(() => props.tone, draw)
</script>

<template>
  <canvas ref="canvas" width="16" height="16" class="block-icon" aria-hidden="true" />
</template>

<style scoped>
.block-icon {
  width: 48px;
  height: 48px;
  image-rendering: pixelated;
  background: #0d0a10;
  box-shadow: 0 0 0 2px #000, inset 0 0 0 2px #2c2430;
}
</style>

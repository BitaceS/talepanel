<script setup lang="ts">
// Hand-drawn pixel night scene for the game theme: sky, moon, pine ridges,
// stone blocks, a campfire and two torches. Drawn procedurally at 320x180 and
// scaled up with nearest-neighbour, so no image assets ship with it.
const canvas = ref<HTMLCanvasElement | null>(null)

const W = 320
const H = 180
const GROUND = 146
const BAYER = [[0, 8, 2, 10], [12, 4, 14, 6], [3, 11, 1, 9], [15, 7, 13, 5]].map(r => r.map(v => (v + 0.5) / 16))

type RGB = [number, number, number]
const hex = (h: string): RGB => [parseInt(h.slice(1, 3), 16), parseInt(h.slice(3, 5), 16), parseInt(h.slice(5, 7), 16)]
const FLAME = ['#fff2b8', '#f2c14e', '#e8863b', '#b9542c', '#7a2b22'].map(hex)

// Lookups the compiler can't prove in range (noUncheckedIndexedAccess); all
// indices here are masked or clamped by the caller.
const bayer = (x: number, y: number) => BAYER[y & 3]![x & 3]!
const flame = (k: number) => FLAME[Math.max(0, Math.min(4, k))]!

function rng(seed: number) {
  return () => {
    seed = (seed + 0x6D2B79F5) | 0
    let t = Math.imul(seed ^ (seed >>> 15), 1 | seed)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

function buildBase(ctx: CanvasRenderingContext2D): ImageData {
  const r = rng(7)
  const img = ctx.createImageData(W, H)
  const d = img.data
  const put = (x: number, y: number, c: RGB) => {
    if (x < 0 || y < 0 || x >= W || y >= H) return
    const i = (y * W + x) * 4
    d[i] = c[0]; d[i + 1] = c[1]; d[i + 2] = c[2]; d[i + 3] = 255
  }
  const get = (x: number, y: number): RGB => {
    const i = (y * W + x) * 4
    return [d[i]!, d[i + 1]!, d[i + 2]!]
  }

  // Sky: hard colour bands with dithered seams
  const sky = ['#110c1d', '#150f25', '#1b132e', '#221636', '#2b1a3b', '#371f3e', '#47243f', '#5b2b3e', '#73343a', '#8e4236'].map(hex)
  for (let y = 0; y < 120; y++) {
    const t = (y / 120) * (sky.length - 1)
    const a = Math.floor(t)
    const f = t - a
    for (let x = 0; x < W; x++) put(x, y, sky[Math.min(a + (f > bayer(x, y) ? 1 : 0), sky.length - 1)]!)
  }

  // Stars
  for (let i = 0; i < 70; i++) {
    const x = Math.floor(r() * W)
    const y = Math.floor(r() * 70)
    put(x, y, r() < 0.3 ? hex('#efe3c8') : hex('#8f8274'))
    if (r() < 0.08) {
      const glow = hex('#5d5470')
      put(x - 1, y, glow); put(x + 1, y, glow); put(x, y - 1, glow); put(x, y + 1, glow)
    }
  }

  // Moon with a dithered halo
  const mx = 244
  const my = 34
  for (let y = -26; y <= 26; y++) {
    for (let x = -26; x <= 26; x++) {
      const dd = Math.sqrt(x * x + y * y)
      if (dd <= 10) {
        put(mx + x, my + y, hex(x + y > 4 ? '#d6c49a' : '#efe3c8'))
      } else if (dd < 26) {
        const f = (1 - (dd - 10) / 16) * 0.55
        if (f > bayer(mx + x, my + y)) {
          const c = get(mx + x, my + y)
          const tint: RGB = [90, 74, 96]
          put(mx + x, my + y, c.map((v, k) => Math.round(v + (tint[k]! - v) * 0.5)) as RGB)
        }
      }
    }
  }
  for (const [a, b] of [[-3, -2], [2, 3], [4, -4], [-4, 4]] as [number, number][]) put(mx + a, my + b, hex('#c9b78d'))

  // Blocky ridges and pines, back to front
  function ridge(base: number, amp: number, step: number, col: string, top: string, seed: number) {
    const rr = rng(seed)
    let h = base
    const hs: number[] = []
    for (let x = 0; x < W; x += step) {
      h += Math.round((rr() - 0.5) * amp)
      h = Math.max(base - amp * 2, Math.min(base + amp, h))
      hs.push(h)
    }
    for (let x = 0; x < W; x++) {
      const hh = hs[Math.floor(x / step)]!
      for (let y = hh; y < H; y++) put(x, y, hex(y === hh ? top : col))
    }
  }
  function pine(px: number, base: number, h: number, c1: string, c2: string) {
    for (let i = 0; i < h; i++) {
      const w = Math.floor((i % 6) / 2 + i / 3) + 1
      const y = base - h + i
      for (let x = -w; x <= w; x++) put(px + x, y, hex(x < -w + 1 ? c2 : c1))
    }
    for (let y = base; y < base + 4; y++) put(px, y, hex('#2a1a12'))
  }
  ridge(92, 10, 6, '#2a1f36', '#3b2c47', 11)
  ridge(108, 8, 5, '#211a2c', '#30253c', 23)
  const pr = rng(5)
  for (let x = -4; x < W + 4; x += 9 + Math.floor(pr() * 8)) pine(x, 124 - Math.floor(pr() * 6), 18 + Math.floor(pr() * 20), '#141c22', '#1f2c33')
  ridge(126, 4, 8, '#191420', '#251d2c', 31)
  for (let x = 4; x < W; x += 14 + Math.floor(pr() * 10)) pine(x, 140, 26 + Math.floor(pr() * 16), '#0f151a', '#1a262d')

  // Ground: 8px blocks, grass on top, stone below
  for (let bx = 0; bx < W; bx += 8) {
    const off = Math.floor(bx / 8) % 5 === 2 ? -4 : 0
    for (let y = GROUND + off; y < H; y++) {
      for (let x = bx; x < bx + 8; x++) {
        const lx = x - bx
        const ly = y - (GROUND + off)
        let c: string
        if (ly < 3) {
          c = ly === 0 ? '#4a5a30' : r() < 0.25 ? '#3d4a2a' : '#2e3a22'
        } else {
          const n = r()
          c = n < 0.15 ? '#2c2431' : n < 0.3 ? '#1d1822' : '#241e29'
          if (ly % 8 === 3 || lx === 0) c = '#16121a'
        }
        put(x, y, hex(c))
      }
    }
  }

  // Campfire stones and logs, torch posts
  const fx = 160
  for (const [x, y, c] of [[-9, -2, '#4a444f'], [-6, -3, '#5c5561'], [6, -3, '#5c5561'], [9, -2, '#4a444f']] as [number, number, string][]) {
    for (let a = 0; a < 3; a++) for (let b = 0; b < 3; b++) put(fx + x + a - 1, GROUND + y + b, hex(c))
  }
  for (let i = -6; i <= 6; i++) {
    put(fx + i, GROUND - 1 - (Math.abs(i) % 2), hex('#5a3a22'))
    put(fx + i, GROUND - 2 + (Math.abs(i) % 2), hex('#3a2414'))
  }
  for (const tx of [64, 256]) {
    for (let y = GROUND - 18; y < GROUND; y++) { put(tx, y, hex('#3a2a1c')); put(tx + 1, y, hex('#2a1d12')) }
    put(tx - 1, GROUND - 18, hex('#6b4a2a')); put(tx + 2, GROUND - 18, hex('#6b4a2a'))
  }
  return img
}

const LIGHTS = [{ x: 160, y: 140, r: 46, s: 1 }, { x: 64.5, y: 124, r: 22, s: 0.7 }, { x: 256.5, y: 124, r: 22, s: 0.7 }]
type Ember = { x: number; y: number; v: number; l: number }

function drawFrame(ctx: CanvasRenderingContext2D, base: ImageData, t: number, embers: Ember[], er: () => number) {
  const img = new ImageData(new Uint8ClampedArray(base.data), W, H)
  const d = img.data
  const put = (x: number, y: number, c: RGB) => {
    x |= 0; y |= 0
    if (x < 0 || y < 0 || x >= W || y >= H) return
    const i = (y * W + x) * 4
    d[i] = c[0]; d[i + 1] = c[1]; d[i + 2] = c[2]
  }

  // Warm light, dithered so it stays on the pixel grid
  const fl = 0.9 + 0.1 * Math.sin(t / 140) + 0.05 * Math.sin(t / 57)
  for (const L of LIGHTS) {
    const R = L.r * fl
    for (let y = Math.max(0, (L.y - R) | 0); y < Math.min(H, L.y + R); y++) {
      for (let x = Math.max(0, (L.x - R) | 0); x < Math.min(W, L.x + R); x++) {
        const dd = Math.hypot(x - L.x, (y - L.y) * 1.3) / R
        if (dd >= 1) continue
        const f = (1 - dd) * L.s
        if (f * 1.15 <= bayer(x, y)) continue
        const i = (y * W + x) * 4
        const step = f > 0.66 ? 0.42 : f > 0.33 ? 0.26 : 0.12
        const r = d[i]!
        const g = d[i + 1]!
        const b = d[i + 2]!
        d[i] = Math.min(255, r + (232 - r) * step)
        d[i + 1] = Math.min(255, g + (134 - g) * step * 0.8)
        d[i + 2] = b + (59 - b) * step * 0.6
      }
    }
  }

  // Campfire flame
  const fr = rng(Math.floor(t / 110))
  for (let y = 0; y < 16; y++) {
    const w = Math.max(0, Math.round((16 - y) / 2.6 + (fr() - 0.5) * 2))
    for (let x = -w; x <= w; x++) {
      const k = Math.min(4, Math.floor((Math.abs(x) / (w + 1)) * 3 + y / 6 + fr() * 0.8))
      if (fr() < 0.08 && y > 8) continue
      put(160 + x, 143 - y, flame(k))
    }
  }

  // Torch flames
  for (const [j, [x, y]] of ([[64, 128], [256, 128]] as [number, number][]).entries()) {
    const tr = rng(Math.floor(t / 130) + j * 31)
    for (let k = 0; k < 5; k++) {
      const w = k < 2 ? 1 : 0
      for (let i = -w; i <= w + 1; i++) put(x + i, y - k - (tr() < 0.3 ? 1 : 0), flame(k + (tr() < 0.4 ? 1 : 0)))
    }
  }

  // Rising embers
  if (er() < 0.35) embers.push({ x: 160 + (er() - 0.5) * 8, y: 132, v: 0.25 + er() * 0.35, l: 0 })
  for (let i = embers.length - 1; i >= 0; i--) {
    const e = embers[i]!
    e.y -= e.v
    e.x += Math.sin(e.l + t / 300) * 0.15
    e.l++
    if (e.l > 110) { embers.splice(i, 1); continue }
    put(e.x, e.y, flame(e.l < 50 ? 1 : 3))
  }

  ctx.putImageData(img, 0, 0)
}

let raf = 0

onMounted(() => {
  const ctx = canvas.value?.getContext('2d')
  if (!ctx) return
  const base = buildBase(ctx)
  const embers: Ember[] = []
  const er = rng(99)

  // Respect "reduce motion": draw one still frame and stop.
  if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
    drawFrame(ctx, base, 0, embers, er)
    return
  }

  let last = 0
  const loop = (t: number) => {
    if (!document.hidden && t - last > 90) {
      drawFrame(ctx, base, t, embers, er)
      last = t
    }
    raf = requestAnimationFrame(loop)
  }
  raf = requestAnimationFrame(loop)
})

onUnmounted(() => cancelAnimationFrame(raf))
</script>

<template>
  <div class="pixel-scene" aria-hidden="true">
    <canvas ref="canvas" :width="W" :height="H" />
  </div>
</template>

<style scoped>
.pixel-scene {
  position: fixed;
  inset: 0;
  z-index: 0;
  pointer-events: none;
}
.pixel-scene canvas {
  width: 100%;
  height: 100%;
  object-fit: cover;
  image-rendering: pixelated;
}
.pixel-scene::after {
  content: '';
  position: absolute;
  inset: 0;
  background: radial-gradient(120% 90% at 50% 45%, transparent 55%, rgba(8, 6, 12, 0.75));
}
</style>

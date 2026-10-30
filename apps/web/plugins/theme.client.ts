// Put the stored theme on <html> before the first page renders, so the game
// theme doesn't flash in as the classic one.
export default defineNuxtPlugin(() => {
  const { theme, apply } = useTheme()
  apply(theme.value)
})

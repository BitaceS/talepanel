export type PanelTheme = 'classic' | 'game'

const STORAGE_KEY = 'tp_theme'

export const PANEL_THEMES: { id: PanelTheme; label: string }[] = [
  { id: 'classic', label: 'Classic' },
  { id: 'game', label: 'Game' },
]

function readStored(): PanelTheme {
  try {
    return localStorage.getItem(STORAGE_KEY) === 'game' ? 'game' : 'classic'
  } catch {
    return 'classic'
  }
}

// Per-browser panel theme. The choice lives in localStorage and is mirrored
// onto <html data-theme="..."> so plain CSS can restyle shared elements.
export function useTheme() {
  const theme = useState<PanelTheme>('panel-theme', readStored)

  function apply(t: PanelTheme) {
    document.documentElement.dataset.theme = t
  }

  function setTheme(t: PanelTheme) {
    theme.value = t
    apply(t)
    try {
      localStorage.setItem(STORAGE_KEY, t)
    } catch {
      // Private mode or blocked storage: the choice lasts for this session.
    }
  }

  return { theme, setTheme, apply }
}

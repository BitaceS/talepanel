// Open state of the sidebar drawer on narrow screens (below Tailwind's md).
// On wider screens the sidebar is always visible and this stays false.
export function useMobileNav() {
  const open = useState('mobile-nav-open', () => false)
  return {
    open,
    toggle: () => { open.value = !open.value },
    close: () => { open.value = false },
  }
}

// Links built from third-party data (CurseForge, GitHub) are only rendered if
// they are plain https URLs, so a javascript: or data: URL can never become
// a clickable href. Returns undefined otherwise, which drops the attribute.
export function httpsHref(url: string | null | undefined): string | undefined {
  return typeof url === 'string' && url.startsWith('https://') ? url : undefined
}

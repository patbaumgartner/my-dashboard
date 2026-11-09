export type Theme = 'light' | 'dark'

const storageKey = 'mydashboard-theme'

function darkSchemeQuery() {
  return window.matchMedia('(prefers-color-scheme: dark)')
}

function storedTheme(): Theme | null {
  try {
    const value = localStorage.getItem(storageKey)
    return value === 'dark' || value === 'light' ? value : null
  } catch {
    return null
  }
}

export function preferredTheme(): Theme {
  return storedTheme() ?? (darkSchemeQuery().matches ? 'dark' : 'light')
}

export function applyDocumentTheme(theme: Theme) {
  document.documentElement.dataset.theme = theme
}

// An explicit choice wins over the operating system from then on.
export function saveTheme(theme: Theme) {
  applyDocumentTheme(theme)
  try {
    localStorage.setItem(storageKey, theme)
  } catch {
    // Private browsing may refuse storage; the choice still lasts for this page.
  }
}

export function followSystemTheme(onChange: (theme: Theme) => void): () => void {
  const query = darkSchemeQuery()
  const listener = (event: MediaQueryListEvent) => {
    if (storedTheme()) return
    const theme: Theme = event.matches ? 'dark' : 'light'
    applyDocumentTheme(theme)
    onChange(theme)
  }
  query.addEventListener('change', listener)
  return () => query.removeEventListener('change', listener)
}

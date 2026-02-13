import { defineStore } from 'pinia'
import { ref, computed, watch } from 'vue'

export const Theme = {
  LIGHT: 'light',
  DARK: 'dark',
  AUTO: 'auto'
}

export const useThemeStore = defineStore('theme', () => {
  const theme = ref(localStorage.getItem('easydo-theme') || Theme.AUTO)
  const systemDark = ref(false)

  const isDark = computed(() => {
    if (theme.value === Theme.AUTO) {
      return systemDark.value
    }
    return theme.value === Theme.DARK
  })

  const currentTheme = computed(() => {
    return isDark.value ? Theme.DARK : Theme.LIGHT
  })

  function init() {
    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
    systemDark.value = mediaQuery.matches

    mediaQuery.addEventListener('change', (e) => {
      systemDark.value = e.matches
      applyTheme()
    })

    applyTheme()
  }

  function setTheme(newTheme) {
    theme.value = newTheme
    localStorage.setItem('easydo-theme', newTheme)
    applyTheme()
  }

  function toggleTheme() {
    const newTheme = isDark.value ? Theme.LIGHT : Theme.DARK
    setTheme(newTheme)
  }

  function applyTheme() {
    const html = document.documentElement
    if (isDark.value) {
      html.classList.add('dark')
      html.classList.remove('light')
    } else {
      html.classList.add('light')
      html.classList.remove('dark')
    }
  }

  watch(isDark, () => {
    applyTheme()
  }, { immediate: true })

  return {
    theme,
    isDark,
    currentTheme,
    Theme,
    init,
    setTheme,
    toggleTheme,
    applyTheme
  }
})

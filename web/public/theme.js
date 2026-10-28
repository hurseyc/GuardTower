// Applied before first paint to avoid a theme flash. (External file: CSP forbids inline scripts.)
try {
  var t = localStorage.getItem('gt-theme') || 'system'
  var dark = t === 'dark' || (t === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.classList.toggle('dark', dark)
} catch (e) {}

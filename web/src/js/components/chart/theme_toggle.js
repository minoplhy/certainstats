// Theme preference is 'system' | 'light' | 'dark'; data-theme always holds the resolved 'light' | 'dark'.
export function initThemeToggle() {
  const root = document.documentElement;
  const media = window.matchMedia ? window.matchMedia('(prefers-color-scheme: dark)') : null;
  const order = ['system', 'light', 'dark'];
  const labels = { system: 'match system', light: 'light', dark: 'dark' };

  function readPref() {
    try {
      const p = localStorage.getItem('certainstats_theme');
      return order.includes(p) ? p : 'system';
    } catch (e) {
      return 'system';
    }
  }

  function apply(pref, notify) {
    const resolved = pref === 'system' ? (media && media.matches ? 'dark' : 'light') : pref;
    const changed = root.getAttribute('data-theme') !== resolved;
    root.setAttribute('data-theme', resolved);
    root.setAttribute('data-theme-pref', pref);
    document.querySelectorAll('#theme-toggle').forEach(btn => {
      const next = order[(order.indexOf(pref) + 1) % order.length];
      btn.setAttribute('data-mode', pref);
      btn.setAttribute('aria-label', 'Theme: ' + labels[pref] + '. Switch to ' + labels[next]);
      btn.title = 'Theme: ' + labels[pref];
    });
    if (notify && changed) {
      window.dispatchEvent(new CustomEvent('certainstats_theme_change', { detail: { theme: resolved } }));
    }
  }

  apply(readPref(), false);

  document.querySelectorAll('#theme-toggle').forEach(btn => {
    btn.addEventListener('click', function () {
      const pref = order[(order.indexOf(readPref()) + 1) % order.length];
      try { localStorage.setItem('certainstats_theme', pref); } catch (e) {}
      apply(pref, true);
    });
  });

  if (media) {
    const onSystemChange = () => { if (readPref() === 'system') apply('system', true); };
    if (media.addEventListener) media.addEventListener('change', onSystemChange);
    else if (media.addListener) media.addListener(onSystemChange);
  }

  const navbar = document.querySelector('.navbar');
  if (navbar) {
    const onScroll = () => navbar.classList.toggle('is-scrolled', window.scrollY > 4);
    window.addEventListener('scroll', onScroll, { passive: true });
    onScroll();
  }
}

// Scroll Position Restoration
export function initScrollRestoration() {
  const scrollPos = sessionStorage.getItem('cs_scroll_pos');
  if (scrollPos) {
    window.scrollTo(0, parseInt(scrollPos, 10));
    sessionStorage.removeItem('cs_scroll_pos');
  }
  window.addEventListener('beforeunload', function () {
    sessionStorage.setItem('cs_scroll_pos', window.scrollY.toString());
  });
}

// Runs once the page has loaded, after window.CertainStatsTelemetry is assigned.
export function initPage() {
  window.CertainStatsTelemetry.initFloatingTooltips();
  window.CertainStatsTelemetry.initScrollRestoration();

  let flash = null;
  try {
    flash = sessionStorage.getItem('certainstats_flash');
    sessionStorage.removeItem('certainstats_flash');
  } catch (e) {}
  if (flash) window.CertainStatsTelemetry.showToast(flash, true);

  const urlParams = new URLSearchParams(window.location.search);
  if (urlParams.has('success')) {
    window.CertainStatsTelemetry.showToast(urlParams.get('success'), true);
  } else if (urlParams.has('error')) {
    window.CertainStatsTelemetry.showToast(urlParams.get('error'), false);
  }
}

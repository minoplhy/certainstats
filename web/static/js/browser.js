(function () {
  'use strict';
  let preference = 'system';
  try { preference = localStorage.getItem('certainstats_theme') || 'system'; } catch (_) {}
  const dark = preference === 'dark' || (preference !== 'light' && window.matchMedia('(prefers-color-scheme: dark)').matches);
  document.documentElement.setAttribute('data-theme', dark ? 'dark' : 'light');
  document.documentElement.setAttribute('data-theme-pref', preference);
  const originalFetch = window.fetch.bind(window);
  window.fetch = function (input, options = {}) {
    const url = new URL(typeof input === 'string' ? input : input.url, location.href);
    const method = (options.method || input.method || 'GET').toUpperCase();
    if (url.origin === location.origin && !['GET', 'HEAD', 'OPTIONS'].includes(method)) {
      options = Object.assign({}, options, { headers: new Headers(options.headers || input.headers) });
      options.headers.set('X-CSRF-Token', document.querySelector('meta[name="csrf-token"]')?.content || '');
    }
    return originalFetch(input, options);
  };
  document.addEventListener('keydown', event => {
    if (event.key === 'Enter' && event.target.matches('[data-click-action="handleAgentItemClick"], [data-click-action="handlePubItemClick"], [data-click-action="startInpageAgentNameEdit"]')) {
      event.preventDefault();
      event.target.click();
    }
  });
  document.addEventListener('DOMContentLoaded',()=>{for(const element of document.querySelectorAll('article[data-click-action], tr[data-click-action]')) {element.tabIndex=0;element.setAttribute('role','link');}});
  const permitted = new Set(['cancelInpageAgentNameEdit', 'cancelInpageExpandedNotesEdit', 'cancelInpageHeaderNoteEdit', 'confirmDeleteDashboard', 'deleteDashboard', 'closeAgentDetail', 'closePublicDetail', 'filterAgents', 'filterAgentsList', 'filterPublicMonitors', 'finishProvisioning', 'handleAgentItemClick', 'handleDestTypeChange', 'handleInpageHeaderNoteClick', 'handlePubItemClick', 'handleTriggerTypeChange', 'onAgentCheckboxChange', 'onAliasChange', 'resetToAlphabetical', 'saveInpageAgentName', 'saveInpageExpandedNotes', 'saveInpageHeaderInlineNote', 'selectProvisionDriver', 'setAgentViewMode', 'setPublicViewMode', 'showInpageReinstallModal', 'showInpageUninstallModal', 'startInpageAgentNameEdit', 'startInpageExpandedNotesEdit', 'submitProvisionAgent', 'toggleAllNodes', 'togglePill', 'toggleSelectAll']);
  for (const type of ['click', 'change', 'input', 'submit']) {
    document.addEventListener(type, function (event) {
      const element = event.target.closest('[data-' + type + '-action]');
      if (!element) return;
      const action = element.getAttribute('data-' + type + '-action');
      if (element.hasAttribute('data-prevent-default')) event.preventDefault();
      if (action === 'confirm') { if (!window.confirm(element.dataset.arg0)) event.preventDefault(); return; }
      if (action === 'node-pill' || action === 'parent-pill') { (action === 'node-pill' ? element.closest('.node-select-pill') : element.parentElement).classList.toggle('active', element.checked); return; }
      if (action === 'save-notes') { event.preventDefault(); document.getElementById('save-notes-btn')?.click(); return; }
      if (!permitted.has(action) || typeof window[action] !== 'function') return;
      const args = [];
      for (let i = 0; i < 4; i++) {
        if (element.hasAttribute('data-arg' + i)) args.push(element.getAttribute('data-arg' + i));
        else if (element.hasAttribute('data-expr' + i)) {
          const expr = element.getAttribute('data-expr' + i);
          args.push(expr === 'event' ? event : expr === 'this' ? element : expr === 'this.checked' ? element.checked : expr === 'this.value' ? element.value : expr === 'true');
        } else break;
      }
      window[action].apply(element, args);
    });
  }
})();

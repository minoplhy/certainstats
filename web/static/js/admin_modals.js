(function () {
  'use strict';

  function deleteDashboard(panelPathOrId, idOrTitle, maybeTitle) {
    let panelPath = '';
    let id = '';
    let title = '';

    if (maybeTitle !== undefined) {
      panelPath = panelPathOrId;
      id = idOrTitle;
      title = maybeTitle;
    } else {
      panelPath = window.CertainStatsTelemetry.getPanelPath();
      id = panelPathOrId;
      title = idOrTitle;
    }

    if (!confirm('Delete public dashboard "' + title + '"?')) return;
    fetch((panelPath || '') + '/dashboard/' + encodeURIComponent(id), {
      method: 'DELETE'
    }).then(res => {
      if (res.ok) {
        window.location.reload();
      } else {
        alert('Failed to delete dashboard');
      }
    }).catch(() => {
      alert('Failed to delete dashboard');
    });
  }

  function initProvisionModal() {
    window.CertainStatsTelemetry.onReady(function() {
      const redirInput = document.getElementById('provision-redirect-to');
      if (redirInput) {
        redirInput.value = window.location.pathname + window.location.search;
      }
    });
  }

  function initSessions() {
    window.CertainStatsTelemetry.onReady(function() {
      if (!window.CertainStatsTelemetry || !window.CertainStatsTelemetry.parseUserAgent) return;
      document.querySelectorAll('.session-row').forEach(function(row) {
        const rawUA = row.getAttribute('data-ua');
        const parsed = window.CertainStatsTelemetry.parseUserAgent(rawUA);
        const labelEl = row.querySelector('.session-ua-label');
        if (labelEl && parsed && parsed.label) {
          labelEl.textContent = parsed.label;
        }
      });
    });
  }

  window.CertainStatsAdminModals = {
    deleteDashboard: deleteDashboard,
    initProvisionModal: initProvisionModal,
    initSessions: initSessions
  };

  // Backwards compatibility globals
  window.deleteDashboard = function(id, title) {
    const publicPath = (document.body?.getAttribute('data-public-path') || '').replace(/\/+$/, '');
    deleteDashboard(publicPath, id, title);
  };
})();

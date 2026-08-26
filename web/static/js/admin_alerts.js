(function () {
  'use strict';

  let panelPath = '';
  let incidentHistory = [];
  let currentFilter = 'all';

  function handleTriggerTypeChange(selectEl, prefix) {
    const val = selectEl.value;
    const threshRow = document.getElementById(prefix + '-threshold-row');
    const unitSpan = document.getElementById(prefix + '-threshold-unit');
    
    if (val === 'agent_down') {
      if (threshRow) threshRow.style.display = 'none';
    } else {
      if (threshRow) threshRow.style.display = 'grid';
      if (unitSpan) {
        if (['net_rx', 'net_tx', 'disk_read', 'disk_write'].includes(val)) {
          unitSpan.textContent = '(KB/s)';
        } else {
          unitSpan.textContent = '(%)';
        }
      }
    }
  }

  function handleDestTypeChange(selectEl, prefix) {
    const val = selectEl.value;
    const presetGroup = document.getElementById(prefix + '-preset-target-group');
    const directGroup = document.getElementById(prefix + '-direct-dest-group');

    if (val === 'preset') {
      if (presetGroup) presetGroup.style.display = 'block';
      if (directGroup) directGroup.style.display = 'none';
    } else {
      if (presetGroup) presetGroup.style.display = 'none';
      if (directGroup) directGroup.style.display = 'block';
    }
  }

  function toggleAllNodes(prefix, check) {
    const container = document.querySelector(`#${prefix}-alert-modal form`);
    if (!container) return;
    container.querySelectorAll('input[name="agents"]').forEach(cb => {
      cb.checked = check;
      const pill = cb.closest('.node-select-pill');
      if (pill) pill.classList.toggle('active', check);
    });
  }

  function formatDuration(triggeredAt, resolvedAt) {
    if (!resolvedAt) return '<span style="color: #ef4444; font-weight: 600;">● Ongoing</span>';
    const diffMs = new Date(resolvedAt).getTime() - new Date(triggeredAt).getTime();
    if (diffMs <= 0) return '< 1s';
    const sec = Math.floor(diffMs / 1000);
    if (sec < 60) return `${sec}s`;
    const min = Math.floor(sec / 60);
    const remSec = sec % 60;
    if (min < 60) return `${min}m ${remSec}s`;
    const hrs = Math.floor(min / 60);
    const remMin = min % 60;
    return `${hrs}h ${remMin}m`;
  }

  function formatBreachVal(item) {
    const type = item.trigger ? item.trigger.type : '';
    if (type === 'agent_down') return '<span style="color: var(--text-muted);">Offline</span>';
    if (['net_rx', 'net_tx', 'disk_read', 'disk_write'].includes(type)) {
      return `${(item.trigger_value || 0).toLocaleString()} KB/s`;
    }
    return `${(item.trigger_value || 0).toFixed(1)}%`;
  }

  function loadHistory() {
    fetch(panelPath + '/api/alerts/history')
      .then(r => r.json())
      .then(data => {
        incidentHistory = Array.isArray(data) ? data : (data.data || data.items || []);
        renderHistory();
      })
      .catch(() => {
        const tbody = document.getElementById('history-tbody');
        if (tbody) {
          tbody.innerHTML = `<tr><td colspan="6" style="text-align: center; color: var(--text-muted); padding: 24px;">No incident records found.</td></tr>`;
        }
      });
  }

  function renderHistory() {
    const tbody = document.getElementById('history-tbody');
    if (!tbody) return;
    const search = (document.getElementById('history-search')?.value || '').toLowerCase();

    const filtered = incidentHistory.filter(item => {
      if (currentFilter === 'firing' && item.resolved_at) return false;
      if (currentFilter === 'resolved' && !item.resolved_at) return false;
      if (search) {
        const text = `${item.alert_nickname || ''} ${item.agent_nickname || ''} ${item.agent_id || ''} ${item.notified_status || ''}`.toLowerCase();
        if (!text.includes(search)) return false;
      }
      return true;
    });

    // Active Incidents Banner
    const firingCount = incidentHistory.filter(i => !i.resolved_at).length;
    const banner = document.getElementById('active-incidents-banner');
    if (banner) {
      if (firingCount > 0) {
        banner.style.display = 'block';
        const countLabel = document.getElementById('incidents-count-label');
        if (countLabel) countLabel.textContent = `${firingCount} Active Firing Incident${firingCount > 1 ? 's' : ''}`;
      } else {
        banner.style.display = 'none';
      }
    }

    if (filtered.length === 0) {
      tbody.innerHTML = `<tr><td colspan="6" style="text-align: center; color: var(--text-muted); padding: 24px;">No incidents matching current criteria.</td></tr>`;
      return;
    }

    tbody.innerHTML = filtered.map(item => {
      const isFiring = !item.resolved_at;
      const trigTime = item.triggered_at ? new Date(item.triggered_at).toLocaleString() : '-';
      const breachVal = formatBreachVal(item);
      const duration = formatDuration(item.triggered_at, item.resolved_at);

      return `
        <tr>
          <td style="font-size: 12px;">${trigTime}</td>
          <td>
            <strong style="color: var(--text-primary);">${item.alert_nickname || 'Rule'}</strong>
            <span style="font-size: 11px; color: var(--text-muted);">(${item.agent_nickname || item.agent_id || 'Node'})</span>
          </td>
          <td class="mono" style="font-size: 12px;">${breachVal}</td>
          <td style="font-size: 12px;">${duration}</td>
          <td>
            <span class="badge ${isFiring ? 'badge-offline' : 'badge-online'}">
              ${isFiring ? 'Firing' : 'Resolved'}
            </span>
          </td>
          <td>
            ${item.notified_status === 'failed' ? `
              <button type="button" class="btn btn-danger btn-sm retry-btn" data-id="${item.history_id}" title="${item.error_message || 'Notification failed'}" style="font-size: 10px; padding: 2px 6px;">
                Retry
              </button>
            ` : `<span style="font-size: 11px; color: var(--status-online);">✓ Delivered</span>`}
          </td>
        </tr>
      `;
    }).join('');

    // Retry Handlers
    tbody.querySelectorAll('.retry-btn').forEach(btn => {
      btn.onclick = function() {
        const incId = this.getAttribute('data-id');
        const origText = this.textContent;
        this.disabled = true;
        this.textContent = 'Queueing...';
        fetch(panelPath + '/api/alerts/history/retry/' + incId, { method: 'POST' })
          .then(r => r.json().then(data => ({ ok: r.ok, data })))
          .then(({ ok, data }) => {
            if (ok) {
              window.CertainStatsTelemetry.showToast(data.message || "Notification retry queued in background", true);
              setTimeout(loadHistory, 1200);
            } else {
              window.CertainStatsTelemetry.showToast(data.message || "Retry request failed", false);
              this.disabled = false;
              this.textContent = origText;
            }
          })
          .catch(() => {
            window.CertainStatsTelemetry.showToast("Network error dispatching retry", false);
            this.disabled = false;
            this.textContent = origText;
          });
      };
    });
  }

  function init(options) {
    options = options || {};
    panelPath = options.panelPath || window.CertainStatsTelemetry.getPanelPath();

    window.CertainStatsTelemetry.onReady(function() {
      loadHistory();

      const searchInput = document.getElementById('history-search');
      if (searchInput) searchInput.oninput = renderHistory;

      document.querySelectorAll('.history-filter-btn').forEach(btn => {
        btn.onclick = function() {
          document.querySelectorAll('.history-filter-btn').forEach(b => b.classList.remove('active'));
          btn.classList.add('active');
          currentFilter = btn.getAttribute('data-status');
          renderHistory();
        };
      });

      // Test Alert Rule Button
      document.querySelectorAll('.test-alert-btn').forEach(btn => {
        btn.onclick = function() {
          const id = this.getAttribute('data-id');
          const origText = this.textContent;
          this.disabled = true;
          this.textContent = 'Testing...';
          fetch(panelPath + '/api/alerts/test', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ alert_id: id })
          })
          .then(r => r.json().then(data => ({ ok: r.ok, data })))
          .then(({ ok, data }) => {
            this.disabled = false;
            this.textContent = origText;
            if (ok) {
              window.CertainStatsTelemetry.showToast(data.message || "Test notification sent successfully", true);
            } else {
              window.CertainStatsTelemetry.showToast(data.message || "Failed to dispatch test notification", false);
            }
          })
          .catch(() => {
            this.disabled = false;
            this.textContent = origText;
            window.CertainStatsTelemetry.showToast("Network error during test notification", false);
          });
        };
      });

      // Test Target Preset Button
      document.querySelectorAll('.test-target-btn').forEach(btn => {
        btn.onclick = function() {
          const id = this.getAttribute('data-id');
          const origText = this.textContent;
          this.disabled = true;
          this.textContent = 'Testing...';
          fetch(panelPath + '/api/alerts/targets/test', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ target_id: id })
          })
          .then(r => r.json().then(data => ({ ok: r.ok, data })))
          .then(({ ok, data }) => {
            this.disabled = false;
            this.textContent = origText;
            if (ok) {
              window.CertainStatsTelemetry.showToast(data.message || "Webhook test verified successfully", true);
            } else {
              window.CertainStatsTelemetry.showToast(data.message || "Webhook test failed", false);
            }
          })
          .catch(() => {
            this.disabled = false;
            this.textContent = origText;
            window.CertainStatsTelemetry.showToast("Network error during webhook test", false);
          });
        };
      });

      // Edit Alert Button
      document.querySelectorAll('.edit-alert-btn').forEach(btn => {
        btn.onclick = function() {
          const id = this.getAttribute('data-id');
          const nickname = this.getAttribute('data-nickname');
          const enabled = this.getAttribute('data-enabled') === 'true';
          const triggerType = this.getAttribute('data-trigger-type') || 'cpu_usage';
          const operator = this.getAttribute('data-operator') || '>';
          const threshold = this.getAttribute('data-threshold') || '90';
          const duration = this.getAttribute('data-duration') || '5m';
          const destType = this.getAttribute('data-dest-type') || 'preset';
          const targetId = this.getAttribute('data-target-id') || '';
          const destination = this.getAttribute('data-destination') || '';
          const payload = this.getAttribute('data-payload') || '';
          const agentsStr = this.getAttribute('data-agents') || '';
          const selectedAgents = agentsStr ? agentsStr.split(',') : [];

          document.getElementById('edit-alert-id').value = id;
          document.getElementById('edit-nickname').value = nickname;
          
          const enabledCheckbox = document.getElementById('edit-enabled');
          if (enabledCheckbox) {
            enabledCheckbox.checked = enabled;
            enabledCheckbox.parentElement.classList.toggle('active', enabled);
          }

          const triggerSelect = document.getElementById('edit-trigger-type');
          if (triggerSelect) {
            triggerSelect.value = triggerType;
            handleTriggerTypeChange(triggerSelect, 'edit');
          }

          document.getElementById('edit-operator').value = operator;
          document.getElementById('edit-threshold').value = threshold;
          document.getElementById('edit-duration').value = duration;

          const destSelect = document.getElementById('edit-dest-type');
          if (destSelect) {
            destSelect.value = destType;
            handleDestTypeChange(destSelect, 'edit');
          }

          const targetSelect = document.getElementById('edit-target-id');
          if (targetSelect && targetId) targetSelect.value = targetId;

          document.getElementById('edit-destination').value = destination;
          document.getElementById('edit-payload').value = payload;

          // Uncheck all edit agents, then check matched ones
          document.querySelectorAll('#edit-alert-modal input[name="agents"]').forEach(cb => {
            const isMatched = selectedAgents.length === 0 || selectedAgents.includes(cb.value);
            cb.checked = isMatched;
            const pill = cb.closest('.node-select-pill');
            if (pill) pill.classList.toggle('active', isMatched);
          });

          document.getElementById('edit-alert-modal').style.display = 'block';
        };
      });

      // Edit Target Button
      document.querySelectorAll('.edit-target-btn').forEach(btn => {
        btn.onclick = function() {
          const id = this.getAttribute('data-id');
          const name = this.getAttribute('data-name');
          const type = this.getAttribute('data-type') || 'discord';
          const destination = this.getAttribute('data-destination') || '';
          const payload = this.getAttribute('data-payload') || '';

          document.getElementById('edit-target-id-input').value = id;
          document.getElementById('edit-target-name').value = name;
          document.getElementById('edit-target-type').value = type;
          document.getElementById('edit-target-destination').value = destination;
          document.getElementById('edit-target-payload').value = payload;

          document.getElementById('edit-target-modal').style.display = 'block';
        };
      });
    });
  }

  window.CertainStatsAdminAlerts = {
    init: init,
    handleTriggerTypeChange: handleTriggerTypeChange,
    handleDestTypeChange: handleDestTypeChange,
    toggleAllNodes: toggleAllNodes,
    loadHistory: loadHistory,
    renderHistory: renderHistory
  };

  // Backwards compatibility globals
  window.handleTriggerTypeChange = handleTriggerTypeChange;
  window.handleDestTypeChange = handleDestTypeChange;
  window.toggleAllNodes = toggleAllNodes;
})();

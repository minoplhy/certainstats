(function () {
  'use strict';

  let panelPath = '';

  function handleTriggerTypeChange(selectEl, prefix) {
    const val = selectEl.value;
    const threshRow = document.getElementById(prefix + '-threshold-row');
    const unitSpan = document.getElementById(prefix + '-threshold-unit');
    
    if (val === 'agent_down') {
      if (threshRow) threshRow.style.display = 'none';
    } else {
      if (threshRow) threshRow.style.display = '';
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
      if (presetGroup) presetGroup.style.display = '';
      if (directGroup) directGroup.style.display = 'none';
    } else {
      if (presetGroup) presetGroup.style.display = 'none';
      if (directGroup) directGroup.style.display = '';
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

  let historyView;
  function loadHistory() { if (historyView) return historyView.refresh(); }

  function init(options) {
    options = options || {};
    panelPath = options.panelPath || window.CertainStatsTelemetry.getPanelPath();

    window.CertainStatsTelemetry.onReady(function() {
      const root = document.querySelector('[data-incident-history]');
      if (root) historyView = window.CertainStatsIncidentHistory.mount(root, { panelPath });

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

          window.CertainStatsModal.open('edit-alert-modal');
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

          window.CertainStatsModal.open('edit-target-modal');
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
    renderHistory: loadHistory
  };

  // Backwards compatibility globals
  window.handleTriggerTypeChange = handleTriggerTypeChange;
  window.handleDestTypeChange = handleDestTypeChange;
  window.toggleAllNodes = toggleAllNodes;
})();

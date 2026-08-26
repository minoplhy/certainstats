(function () {
  'use strict';

  let selectedAgentsOrder = [];
  let isDragged = false;
  let isCreate = false;
  let dashboardId = '';
  let panelPath = '';
  let draggedItem = null;

  function togglePill(input) {
    const pill = input.closest('.toggle-pill');
    if (input.checked) {
      pill.classList.add('active');
    } else {
      pill.classList.remove('active');
    }
  }

  function filterAgents() {
    const query = (document.getElementById('agent-search')?.value || '').toLowerCase();
    document.querySelectorAll('.agent-select-row').forEach(row => {
      const name = (row.getAttribute('data-agent-name') || '').toLowerCase();
      const id = (row.getAttribute('data-agent-id') || '').toLowerCase();
      if (name.includes(query) || id.includes(query)) {
        row.style.display = 'flex';
      } else {
        row.style.display = 'none';
      }
    });
  }

  function toggleSelectAll() {
    const visibleRows = Array.from(document.querySelectorAll('.agent-select-row')).filter(r => r.style.display !== 'none');
    const allChecked = visibleRows.every(r => r.querySelector('input[type="checkbox"]').checked);
    
    visibleRows.forEach(row => {
      const cb = row.querySelector('input[type="checkbox"]');
      const agentId = row.getAttribute('data-agent-id');
      cb.checked = !allChecked;
      onAgentCheckboxChange(agentId, cb.checked);
    });
  }

  function onAgentCheckboxChange(agentId, checked) {
    const aliasBox = document.getElementById('alias-box-' + agentId);
    const aliasInput = document.getElementById('alias-input-' + agentId);
    if (aliasBox) {
      aliasBox.style.display = checked ? 'flex' : 'none';
    }
    if (aliasInput) {
      aliasInput.disabled = !checked;
    }

    if (checked) {
      if (!selectedAgentsOrder.includes(agentId)) {
        selectedAgentsOrder.push(agentId);
      }
    } else {
      selectedAgentsOrder = selectedAgentsOrder.filter(id => id !== agentId);
    }

    updateReorderSection();
  }

  function onAliasChange(agentId, newAlias) {
    updateReorderSection();
  }

  function escapeHtml(str) {
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
  }

  function setupDragEvents(el) {
    el.addEventListener('dragstart', function(e) {
      draggedItem = this;
      this.classList.add('dragging');
      e.dataTransfer.effectAllowed = 'move';
    });

    el.addEventListener('dragend', function() {
      this.classList.remove('dragging');
      draggedItem = null;
    });

    el.addEventListener('dragover', function(e) {
      e.preventDefault();
      if (!draggedItem || draggedItem === this) return;

      const list = document.getElementById('reorder-list');
      const items = Array.from(list.querySelectorAll('.reorder-item'));
      const fromIndex = items.indexOf(draggedItem);
      const toIndex = items.indexOf(this);

      if (fromIndex !== -1 && toIndex !== -1 && fromIndex !== toIndex) {
        const movedId = selectedAgentsOrder[fromIndex];
        selectedAgentsOrder.splice(fromIndex, 1);
        selectedAgentsOrder.splice(toIndex, 0, movedId);
        isDragged = true;
        updateReorderSection();
      }
    });
  }

  function syncOrderInput() {
    const orderInput = document.getElementById('agents-order-input');
    const draggedInput = document.getElementById('is-dragged-input');
    if (orderInput) orderInput.value = selectedAgentsOrder.join(',');
    if (draggedInput) draggedInput.value = isDragged ? '1' : '0';
  }

  function updateReorderSection() {
    const reorderSec = document.getElementById('reorder-section');
    const reorderList = document.getElementById('reorder-list');
    const btnReset = document.getElementById('btn-reset-order');
    const badge = document.getElementById('selected-agents-badge');

    if (badge) {
      badge.textContent = selectedAgentsOrder.length + ' Selected';
      badge.style.display = selectedAgentsOrder.length > 0 ? 'inline-block' : 'none';
    }

    if (!reorderSec || !reorderList) return;

    if (selectedAgentsOrder.length <= 1) {
      reorderSec.style.display = 'none';
      syncOrderInput();
      return;
    }

    reorderSec.style.display = 'block';
    if (btnReset) btnReset.style.display = isDragged ? 'inline-block' : 'none';

    reorderList.innerHTML = '';
    selectedAgentsOrder.forEach((agentId, index) => {
      const agentRow = document.querySelector(`.agent-select-row[data-agent-id="${agentId}"]`);
      const origName = agentRow ? (agentRow.getAttribute('data-agent-name') || agentId) : agentId;
      const aliasInput = document.getElementById('alias-input-' + agentId);
      const alias = aliasInput ? (aliasInput.value || origName) : origName;

      const item = document.createElement('div');
      item.className = 'reorder-item';
      item.setAttribute('draggable', 'true');
      item.setAttribute('data-agent-id', agentId);
      item.setAttribute('data-index', index);

      item.innerHTML = `
        <span style="color: var(--text-muted); cursor: grab; font-size: 14px;">⋮⋮</span>
        <span style="font-size: 10px; font-weight: 700; background: var(--bg-secondary); padding: 2px 6px; border-radius: 4px; color: var(--text-muted); font-family: var(--font-mono);">#${index + 1}</span>
        <div style="flex: 1; display: flex; justify-content: space-between; align-items: center;">
          <span style="font-size: 12px; font-weight: 600; color: var(--text-primary);">${escapeHtml(alias)}</span>
          <span style="font-size: 10px; color: var(--text-muted); font-family: var(--font-mono);">${escapeHtml(agentId)}</span>
        </div>
      `;

      setupDragEvents(item);
      reorderList.appendChild(item);
    });

    syncOrderInput();
  }

  function resetToAlphabetical() {
    selectedAgentsOrder.sort((aId, bId) => {
      const rowA = document.querySelector(`.agent-select-row[data-agent-id="${aId}"]`);
      const rowB = document.querySelector(`.agent-select-row[data-agent-id="${bId}"]`);
      const nameA = (rowA ? rowA.getAttribute('data-agent-name') : aId).toLowerCase();
      const nameB = (rowB ? rowB.getAttribute('data-agent-name') : bId).toLowerCase();
      return nameA.localeCompare(nameB);
    });
    isDragged = false;
    updateReorderSection();
  }

  function confirmDeleteDashboard() {
    fetch((panelPath || '') + '/dashboard/' + encodeURIComponent(dashboardId), {
      method: 'DELETE'
    }).then(res => {
      if (res.ok) {
        window.location.href = (panelPath || '') + '/dashboards';
      } else {
        alert('Failed to delete dashboard');
      }
    }).catch(() => {
      alert('Failed to delete dashboard');
    });
  }

  function init(options) {
    options = options || {};
    selectedAgentsOrder = options.selectedAgentsOrder || [];
    isDragged = !!options.isDragged;
    isCreate = !!options.isCreate;
    dashboardId = options.dashboardId || '';
    panelPath = options.panelPath || window.CertainStatsTelemetry.getPanelPath();

    window.CertainStatsTelemetry.onReady(function() {
      // Live Slug auto-generation on create
      if (isCreate) {
        const titleInput = document.getElementById('dash-title-input');
        const slugInput = document.getElementById('dash-slug-input');
        if (titleInput && slugInput) {
          titleInput.addEventListener('input', function() {
            slugInput.value = this.value.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/(^-|-$)/g, '');
          });
        }
      }

      updateReorderSection();
    });
  }

  window.CertainStatsDashboardEdit = {
    init: init,
    togglePill: togglePill,
    filterAgents: filterAgents,
    toggleSelectAll: toggleSelectAll,
    onAgentCheckboxChange: onAgentCheckboxChange,
    onAliasChange: onAliasChange,
    updateReorderSection: updateReorderSection,
    resetToAlphabetical: resetToAlphabetical,
    confirmDeleteDashboard: confirmDeleteDashboard
  };

  // Backwards compatibility globals
  window.togglePill = togglePill;
  window.filterAgents = filterAgents;
  window.toggleSelectAll = toggleSelectAll;
  window.onAgentCheckboxChange = onAgentCheckboxChange;
  window.onAliasChange = onAliasChange;
  window.updateReorderSection = updateReorderSection;
  window.resetToAlphabetical = resetToAlphabetical;
  window.confirmDeleteDashboard = confirmDeleteDashboard;
})();

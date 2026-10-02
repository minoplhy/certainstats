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

    // Ensure all checked checkboxes are represented in selectedAgentsOrder
    document.querySelectorAll('input[type="checkbox"][name="agents"]:checked').forEach(function(cb) {
      if (cb.value && !selectedAgentsOrder.includes(cb.value)) {
        selectedAgentsOrder.push(cb.value);
      }
    });

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

  function getDragAfterElement(container, y) {
    const draggableElements = Array.from(container.querySelectorAll('.reorder-item:not(.dragging)'));

    return draggableElements.reduce((closest, child) => {
      const box = child.getBoundingClientRect();
      const offset = y - box.top - box.height / 2;
      if (offset < 0 && offset > closest.offset) {
        return { offset: offset, element: child };
      } else {
        return closest;
      }
    }, { offset: Number.NEGATIVE_INFINITY }).element;
  }

  function updateOrderBadges() {
    const list = document.getElementById('reorder-list');
    if (!list) return;
    const items = list.querySelectorAll('.reorder-item');
    items.forEach((item, index) => {
      item.setAttribute('data-index', index);
      const badge = item.querySelector('.reorder-badge');
      if (badge) {
        badge.textContent = '#' + (index + 1);
      }
    });
  }

  function finalizeReorder() {
    const list = document.getElementById('reorder-list');
    if (!list) return;
    const items = Array.from(list.querySelectorAll('.reorder-item'));
    const newOrder = items.map(el => el.getAttribute('data-agent-id')).filter(Boolean);
    if (newOrder.length > 0 && newOrder.join(',') !== selectedAgentsOrder.join(',')) {
      selectedAgentsOrder = newOrder;
      isDragged = true;
      updateReorderSection(); // re-render so badges and move-button states match the new order
      return;
    }
    updateOrderBadges();
  }

  function setupDragEvents(el) {
    el.addEventListener('dragstart', function(e) {
      draggedItem = this;
      this.classList.add('dragging');
      e.dataTransfer.effectAllowed = 'move';
      e.dataTransfer.setData('text/plain', this.getAttribute('data-agent-id'));
    });

    el.addEventListener('dragend', function() {
      this.classList.remove('dragging');
      draggedItem = null;
      finalizeReorder();
    });

    el.addEventListener('dragover', function(e) {
      e.preventDefault();
      e.dataTransfer.dropEffect = 'move';
    });
  }

  function syncOrderInput() {
    const container = document.getElementById('agents-order-container');
    if (container) {
      container.innerHTML = '';
      selectedAgentsOrder.forEach(function(agentId) {
        if (agentId && agentId.trim()) {
          const input = document.createElement('input');
          input.type = 'hidden';
          input.name = 'agents_order';
          input.value = agentId.trim();
          container.appendChild(input);
        }
      });
    }
    const draggedInput = document.getElementById('is-dragged-input');
    if (draggedInput) draggedInput.value = isDragged ? '1' : '0';
    window.selectedAgentsOrder = selectedAgentsOrder;
    window.agents_order = selectedAgentsOrder;
  }

  function ensureReorderListDragEvents(list) {
    if (!list || list.__dragEventsAttached) return;
    list.__dragEventsAttached = true;

    list.addEventListener('dragover', function(e) {
      e.preventDefault();
      e.dataTransfer.dropEffect = 'move';
      if (!draggedItem) return;

      const afterElement = getDragAfterElement(list, e.clientY);
      if (afterElement == null) {
        if (list.lastElementChild !== draggedItem) {
          list.appendChild(draggedItem);
          updateOrderBadges();
        }
      } else if (afterElement !== draggedItem && afterElement !== draggedItem.nextSibling) {
        list.insertBefore(draggedItem, afterElement);
        updateOrderBadges();
      }
    });

    list.addEventListener('click', function(e) {
      const btn = e.target.closest('[data-move]');
      if (!btn) return;
      const item = btn.closest('.reorder-item');
      const from = selectedAgentsOrder.indexOf(item.getAttribute('data-agent-id'));
      const to = from + parseInt(btn.getAttribute('data-move'), 10);
      if (from < 0 || to < 0 || to >= selectedAgentsOrder.length) return;
      const moved = selectedAgentsOrder.splice(from, 1)[0];
      selectedAgentsOrder.splice(to, 0, moved);
      isDragged = true;
      updateReorderSection();
      const again = list.querySelector('.reorder-item[data-agent-id="' + CSS.escape(moved) + '"] [data-move="' + btn.getAttribute('data-move') + '"]');
      if (again && !again.disabled) again.focus();
    });

    list.addEventListener('drop', function(e) {
      e.preventDefault();
      if (draggedItem) {
        draggedItem.classList.remove('dragging');
        draggedItem = null;
      }
      finalizeReorder();
    });
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

    if (!reorderSec || !reorderList) {
      syncOrderInput();
      return;
    }

    ensureReorderListDragEvents(reorderList);

    if (selectedAgentsOrder.length <= 1) {
      reorderSec.style.display = 'none';
      syncOrderInput();
      return;
    }

    reorderSec.style.display = 'block';
    if (btnReset) btnReset.style.display = isDragged ? 'inline-flex' : 'none';

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
        <svg class="reorder-grip" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><circle cx="9" cy="6" r="1.6"/><circle cx="15" cy="6" r="1.6"/><circle cx="9" cy="12" r="1.6"/><circle cx="15" cy="12" r="1.6"/><circle cx="9" cy="18" r="1.6"/><circle cx="15" cy="18" r="1.6"/></svg>
        <span class="reorder-badge">#${index + 1}</span>
        <span class="reorder-name">
          <strong>${escapeHtml(alias)}</strong>
          <span class="mono">${escapeHtml(agentId)}</span>
        </span>
        <span class="reorder-moves">
          <button type="button" class="btn btn-ghost btn-sm" data-move="-1" aria-label="Move ${escapeHtml(alias)} up"${index === 0 ? ' disabled' : ''}><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 15l6-6 6 6"/></svg></button>
          <button type="button" class="btn btn-ghost btn-sm" data-move="1" aria-label="Move ${escapeHtml(alias)} down"${index === selectedAgentsOrder.length - 1 ? ' disabled' : ''}><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 9l6 6 6-6"/></svg></button>
        </span>
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
    fetch((panelPath || '') + '/dashboards/' + encodeURIComponent(dashboardId), {
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
    const initialOrder = options.selectedAgentsOrder || options.agents_order || options.agentsOrder || options.agents || [];
    selectedAgentsOrder = initialOrder.filter(function(id) {
      return typeof id === 'string' && id.trim().length > 0;
    });
    isDragged = !!options.isDragged;
    isCreate = !!options.isCreate;
    dashboardId = options.dashboardId || '';
    panelPath = options.panelPath || (window.CertainStatsTelemetry && window.CertainStatsTelemetry.getPanelPath ? window.CertainStatsTelemetry.getPanelPath() : '');

    // Fallback: ensure all currently checked checkboxes are present in selectedAgentsOrder
    document.querySelectorAll('input[type="checkbox"][name="agents"]:checked').forEach(function(cb) {
      if (cb.value && !selectedAgentsOrder.includes(cb.value)) {
        selectedAgentsOrder.push(cb.value);
      }
    });

    updateReorderSection();

    // Hook form submit to ensure all currently checked agents are synced to agents-order-container
    const form = document.getElementById('dashboard-form');
    if (form) {
      form.addEventListener('submit', function() {
        document.querySelectorAll('input[type="checkbox"][name="agents"]:checked').forEach(function(cb) {
          if (cb.value && !selectedAgentsOrder.includes(cb.value)) {
            selectedAgentsOrder.push(cb.value);
          }
        });
        syncOrderInput();
      });
    }

    if (window.CertainStatsTelemetry && window.CertainStatsTelemetry.onReady) {
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
    confirmDeleteDashboard: confirmDeleteDashboard,
    get selectedAgentsOrder() { return selectedAgentsOrder; },
    get agents_order() { return selectedAgentsOrder; },
    get agents() { return selectedAgentsOrder; }
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
  window.selectedAgentsOrder = selectedAgentsOrder;
  window.agents_order = selectedAgentsOrder;
})();

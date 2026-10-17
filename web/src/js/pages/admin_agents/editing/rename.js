import { state } from '../state.js';
import { cancelInpageHeaderNoteEdit, cancelInpageExpandedNotesEdit } from './notes.js';

// Renaming an agent from its detail header.

export function startInpageAgentNameEdit() {
  if (!state.currentActiveAgentId) return;
  const agent = state.agentsData.find(a => a.agent_id === state.currentActiveAgentId);
  const readEl = document.getElementById('detail-name-read-container');
  const editEl = document.getElementById('detail-name-edit-container');
  const input = document.getElementById('detail-name-input');
  if (readEl) readEl.hidden = true;
  if (editEl) editEl.hidden = false;
  if (input) {
    input.value = agent ? (agent.nickname || agent.agent_id) : '';
    input.focus();
    input.select();
  }
}

export function cancelInpageAgentNameEdit() {
  const readEl = document.getElementById('detail-name-read-container');
  const editEl = document.getElementById('detail-name-edit-container');
  if (editEl) editEl.hidden = true;
  if (readEl) readEl.hidden = false;
}

export function saveInpageAgentName() {
  if (!state.currentActiveAgentId) return;
  const input = document.getElementById('detail-name-input');
  const saveBtn = document.getElementById('detail-name-save-btn');
  const newNickname = input ? input.value.trim() : '';

  if (!newNickname) {
    window.CertainStatsTelemetry.showToast('Nickname cannot be empty', false);
    if (input) input.focus();
    return;
  }
  if (newNickname.length > 64) {
    window.CertainStatsTelemetry.showToast('Nickname cannot exceed 64 characters', false);
    if (input) input.focus();
    return;
  }

  if (saveBtn) {
    saveBtn.disabled = true;
    saveBtn.textContent = 'Saving...';
  }

  fetch(state.panelPath + '/api/agent', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ agent_id: state.currentActiveAgentId, nickname: newNickname })
  }).then(r => {
    if (saveBtn) {
      saveBtn.disabled = false;
      saveBtn.textContent = 'Save';
    }
    if (r.ok) {
      const agent = state.agentsData.find(a => a.agent_id === state.currentActiveAgentId);
      if (agent) {
        agent.nickname = newNickname;
      }

      // Update active name in header
      const activeNameEl = document.getElementById('detail-active-name');
      if (activeNameEl) activeNameEl.textContent = newNickname;

      // Update overview grid card
      document.querySelectorAll('.agent-card-item[data-agent-id="' + state.currentActiveAgentId + '"]').forEach(card => {
        card.setAttribute('data-agent-name', newNickname);
        const nameSpan = card.querySelector('.agent-name span:not(.status-dot)');
        if (nameSpan) nameSpan.textContent = newNickname;
      });

      // Update overview list row
      document.querySelectorAll('.agent-row-item[data-agent-id="' + state.currentActiveAgentId + '"]').forEach(row => {
        row.setAttribute('data-agent-name', newNickname);
        const nameStrong = row.querySelector('.agent-name strong');
        if (nameStrong) nameStrong.textContent = newNickname;
      });

      cancelInpageAgentNameEdit();
      window.CertainStatsTelemetry.showToast('Agent renamed to ' + newNickname, true);
    } else {
      r.json().then(errData => {
        window.CertainStatsTelemetry.showToast(errData.error || 'Failed to rename agent', false);
      }).catch(() => {
        window.CertainStatsTelemetry.showToast('Failed to rename agent', false);
      });
    }
  }).catch(() => {
    if (saveBtn) {
      saveBtn.disabled = false;
      saveBtn.textContent = 'Save';
    }
    window.CertainStatsTelemetry.showToast('Network error renaming agent', false);
  });
}

export function resetInpageEditStates() {
  cancelInpageAgentNameEdit();
  cancelInpageHeaderNoteEdit();
  cancelInpageExpandedNotesEdit();

  // Close any open modals
  ['reinstall-modal', 'uninstall-modal', 'add-agent-modal'].forEach(id => window.CertainStatsModal.close(id));
}

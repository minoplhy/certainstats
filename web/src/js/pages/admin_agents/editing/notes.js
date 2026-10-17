import { state } from '../state.js';

// Agent notes in the detail header and the expanded notes section.

export function isLongNote(note) {
  return !!(note && (note.length > 40 || note.includes('\n')));
}

export function syncInpageNotesUI(note) {
  const isLong = isLongNote(note);
  const headerReadEl = document.getElementById('inpage-header-notes-read');
  const headerEditEl = document.getElementById('inpage-header-notes-edit');
  const headerTextEl = document.getElementById('inpage-header-notes-text');
  const expandedSection = document.getElementById('inpage-agent-notes-section');
  const expandedReadEl = document.getElementById('inpage-expanded-notes-read');
  const expandedEditEl = document.getElementById('inpage-expanded-notes-edit');
  const expandedTextEl = document.getElementById('inpage-expanded-notes-text');
  const expandedTextarea = document.getElementById('inpage-expanded-notes-textarea');
  const headerInput = document.getElementById('inpage-header-notes-input');

  if (headerReadEl) headerReadEl.hidden = false;
  if (headerEditEl) headerEditEl.hidden = true;

  if (headerTextEl) {
    if (isLong) {
      headerTextEl.textContent = 'Notes below';
      headerTextEl.title = 'Jump to notes';
    } else {
      headerTextEl.textContent = note || 'Add a note';
      headerTextEl.title = note ? 'Edit note' : 'Add a private note';
    }
    headerTextEl.classList.toggle('is-empty', !note);
  }

  if (headerInput) headerInput.value = note || '';

  if (expandedSection) {
    expandedSection.hidden = !isLong;
    if (expandedReadEl) expandedReadEl.hidden = false;
    if (expandedEditEl) expandedEditEl.hidden = true;
    if (expandedTextEl) expandedTextEl.textContent = note || '';
    if (expandedTextarea) expandedTextarea.value = note || '';
  }
}

export function handleInpageHeaderNoteClick() {
  if (!state.currentActiveAgentId) return;
  const agent = state.agentsData.find(a => a.agent_id === state.currentActiveAgentId);
  const note = agent ? (agent.note || '') : '';
  if (isLongNote(note)) {
    const el = document.getElementById('inpage-agent-notes-section');
    if (el) el.scrollIntoView({ behavior: 'smooth' });
  } else {
    const readEl = document.getElementById('inpage-header-notes-read');
    const editEl = document.getElementById('inpage-header-notes-edit');
    const input = document.getElementById('inpage-header-notes-input');
    if (readEl) readEl.hidden = true;
    if (editEl) editEl.hidden = false;
    if (input) {
      input.value = note;
      input.focus();
    }
  }
}

export function cancelInpageHeaderNoteEdit() {
  const readEl = document.getElementById('inpage-header-notes-read');
  const editEl = document.getElementById('inpage-header-notes-edit');
  if (editEl) editEl.hidden = true;
  if (readEl) readEl.hidden = false;
}

export function saveInpageHeaderInlineNote() {
  const input = document.getElementById('inpage-header-notes-input');
  const noteText = input ? input.value.trim() : '';
  saveInpageNote(noteText);
}

export function startInpageExpandedNotesEdit() {
  if (!state.currentActiveAgentId) return;
  const agent = state.agentsData.find(a => a.agent_id === state.currentActiveAgentId);
  const readEl = document.getElementById('inpage-expanded-notes-read');
  const editEl = document.getElementById('inpage-expanded-notes-edit');
  const textarea = document.getElementById('inpage-expanded-notes-textarea');
  if (readEl) readEl.hidden = true;
  if (editEl) editEl.hidden = false;
  if (textarea) {
    textarea.value = agent ? (agent.note || '') : '';
    textarea.focus();
  }
}

export function cancelInpageExpandedNotesEdit() {
  if (!state.currentActiveAgentId) return;
  const agent = state.agentsData.find(a => a.agent_id === state.currentActiveAgentId);
  const readEl = document.getElementById('inpage-expanded-notes-read');
  const editEl = document.getElementById('inpage-expanded-notes-edit');
  const textarea = document.getElementById('inpage-expanded-notes-textarea');
  if (textarea) textarea.value = agent ? (agent.note || '') : '';
  if (editEl) editEl.hidden = true;
  if (readEl) readEl.hidden = false;
}

export function saveInpageExpandedNotes() {
  const textarea = document.getElementById('inpage-expanded-notes-textarea');
  const saveBtn = document.getElementById('inpage-btn-save-expanded-notes');
  const noteText = textarea ? textarea.value : '';
  saveInpageNote(noteText, saveBtn);
}

export function saveInpageNote(noteText, btnEl) {
  if (!state.currentActiveAgentId) return;
  if (btnEl) {
    btnEl.disabled = true;
    btnEl.textContent = 'Saving...';
  }

  fetch(state.panelPath + '/api/agent', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ agent_id: state.currentActiveAgentId, note: noteText })
  }).then(r => {
    if (btnEl) {
      btnEl.disabled = false;
      btnEl.textContent = 'Save Note';
    }
    if (r.ok) {
      const agent = state.agentsData.find(a => a.agent_id === state.currentActiveAgentId);
      if (agent) agent.note = noteText;
      syncInpageNotesUI(noteText);
      window.CertainStatsTelemetry.showToast('Note saved successfully', true);
    } else {
      window.CertainStatsTelemetry.showToast('Failed to save note', false);
    }
  }).catch(() => {
    if (btnEl) {
      btnEl.disabled = false;
      btnEl.textContent = 'Save Note';
    }
    window.CertainStatsTelemetry.showToast('Network error saving note', false);
  });
}

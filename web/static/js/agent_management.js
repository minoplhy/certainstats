(function () {
  'use strict';

  let panelPath = '';
  let activeEditingMgmtAgentId = null;

  function copyCredential(text, label) {
    navigator.clipboard.writeText(text).then(() => {
      if (window.CertainStatsTelemetry && window.CertainStatsTelemetry.showToast) {
        window.CertainStatsTelemetry.showToast(label + ' copied to clipboard');
      } else {
        alert(label + ' copied to clipboard');
      }
    }).catch(() => {
      prompt('Copy ' + label + ':', text);
    });
  }

  function showReinstallModal(agentId, nickname, agentType, token, sshKey) {
    window.CertainStatsTelemetry.openReinstallModal({
      agentId: agentId,
      nickname: nickname,
      agentType: agentType,
      panelPath: panelPath,
      sshKey: sshKey
    });
  }

  function startMgmtAgentRename(agentId) {
    if (activeEditingMgmtAgentId && activeEditingMgmtAgentId !== agentId) {
      cancelMgmtAgentRename(activeEditingMgmtAgentId);
    }
    activeEditingMgmtAgentId = agentId;

    const readEl = document.getElementById('mgmt-name-read-' + agentId);
    const editEl = document.getElementById('mgmt-name-edit-' + agentId);
    const input = document.getElementById('mgmt-name-input-' + agentId);
    if (readEl) readEl.hidden = true;
    if (editEl) editEl.hidden = false;
    if (input) {
      input.focus();
      input.select();
      if (!input._hasRenameKeyHandler) {
        input._hasRenameKeyHandler = true;
        input.addEventListener('keydown', function (e) {
          if (e.key === 'Enter') saveMgmtAgentRename(agentId);
          if (e.key === 'Escape') cancelMgmtAgentRename(agentId);
        });
      }
    }
  }

  function cancelMgmtAgentRename(agentId) {
    if (activeEditingMgmtAgentId === agentId) {
      activeEditingMgmtAgentId = null;
    }
    const readEl = document.getElementById('mgmt-name-read-' + agentId);
    const editEl = document.getElementById('mgmt-name-edit-' + agentId);
    if (editEl) editEl.hidden = true;
    if (readEl) readEl.hidden = false;
  }

  function saveMgmtAgentRename(agentId) {
    const input = document.getElementById('mgmt-name-input-' + agentId);
    const saveBtn = document.getElementById('mgmt-name-save-btn-' + agentId);
    const newNickname = input ? input.value.trim() : '';

    if (!newNickname) {
      if (window.CertainStatsTelemetry && window.CertainStatsTelemetry.showToast) {
        window.CertainStatsTelemetry.showToast('Nickname cannot be empty', false);
      } else {
        alert('Nickname cannot be empty');
      }
      if (input) input.focus();
      return;
    }
    if (newNickname.length > 64) {
      if (window.CertainStatsTelemetry && window.CertainStatsTelemetry.showToast) {
        window.CertainStatsTelemetry.showToast('Nickname cannot exceed 64 characters', false);
      } else {
        alert('Nickname cannot exceed 64 characters');
      }
      if (input) input.focus();
      return;
    }

    if (saveBtn) {
      saveBtn.disabled = true;
      saveBtn.textContent = 'Saving...';
    }

    const effectivePanelPath = panelPath || (window.CertainStatsTelemetry && window.CertainStatsTelemetry.getPanelPath ? window.CertainStatsTelemetry.getPanelPath() : '');
    fetch(effectivePanelPath + '/api/agent', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ agent_id: agentId, nickname: newNickname })
    }).then(r => {
      if (saveBtn) {
        saveBtn.disabled = false;
        saveBtn.textContent = 'Save';
      }
      if (r.ok) {
        const item = document.querySelector('[data-mgmt-agent-id="' + agentId + '"]');
        if (item) {
          item.querySelectorAll('.agent-mgmt-nickname').forEach(el => { el.textContent = newNickname; });
          const badge = item.querySelector('.mgmt-summary .badge');
          item.setAttribute('data-search', [newNickname, agentId, badge ? badge.textContent : ''].join(' '));
        }
        cancelMgmtAgentRename(agentId);
        if (window.CertainStatsTelemetry && window.CertainStatsTelemetry.showToast) {
          window.CertainStatsTelemetry.showToast('Agent renamed to ' + newNickname, true);
        }
      } else {
        r.json().then(errData => {
          const msg = (errData && errData.error) || 'Failed to rename agent';
          if (window.CertainStatsTelemetry && window.CertainStatsTelemetry.showToast) {
            window.CertainStatsTelemetry.showToast(msg, false);
          } else {
            alert(msg);
          }
        }).catch(() => {
          if (window.CertainStatsTelemetry && window.CertainStatsTelemetry.showToast) {
            window.CertainStatsTelemetry.showToast('Failed to rename agent', false);
          }
        });
      }
    }).catch(() => {
      if (saveBtn) {
        saveBtn.disabled = false;
        saveBtn.textContent = 'Save';
      }
      if (window.CertainStatsTelemetry && window.CertainStatsTelemetry.showToast) {
        window.CertainStatsTelemetry.showToast('Network error renaming agent', false);
      }
    });
  }

  function toggleSecret(btn) {
    const code = btn.parentElement.querySelector('code[data-secret]');
    if (!code) return;
    const show = btn.getAttribute('aria-pressed') !== 'true';
    code.textContent = show ? code.getAttribute('data-secret') : code.getAttribute('data-mask');
    code.classList.toggle('is-revealed', show);
    btn.setAttribute('aria-pressed', String(show));
    btn.textContent = show ? 'Hide' : 'Show';
  }

  function filterMgmtList() {
    const input = document.getElementById('mgmt-search-input');
    const query = (input ? input.value : '').trim().toLowerCase();
    let shown = 0;
    document.querySelectorAll('.mgmt-item').forEach(el => {
      const match = (el.getAttribute('data-search') || '').toLowerCase().includes(query);
      el.hidden = !match;
      if (match) shown++;
    });
    const empty = document.getElementById('mgmt-no-match');
    if (empty) empty.hidden = shown > 0;
  }

  const actions = {
    'rename': btn => startMgmtAgentRename(btn.dataset.agentId),
    'rename-save': btn => saveMgmtAgentRename(btn.dataset.agentId),
    'rename-cancel': btn => cancelMgmtAgentRename(btn.dataset.agentId),
    'copy': btn => copyCredential(btn.dataset.secret, btn.dataset.label),
    'toggle-secret': toggleSecret,
    'install': btn => showReinstallModal(btn.dataset.agentId, btn.dataset.nickname, btn.dataset.agentType, '', btn.dataset.sshKey),
    'uninstall': btn => window.CertainStatsTelemetry.openUninstallModal({ agentId: btn.dataset.agentId, nickname: btn.dataset.nickname })
  };

  // After a reset the server redirects to #agent-{id}: reopen that row and point at it.
  function openFromHash() {
    const id = decodeURIComponent(window.location.hash.slice(1));
    if (!id.startsWith('agent-')) return;
    const item = document.getElementById(id);
    if (!item || !item.classList.contains('mgmt-item')) return;
    item.open = true;
    item.scrollIntoView({ block: 'center' });
    item.classList.add('is-highlighted');
    setTimeout(() => item.classList.remove('is-highlighted'), 2400);
  }

  function init(options) {
    options = options || {};
    panelPath = options.panelPath || window.CertainStatsTelemetry.getPanelPath();

    document.addEventListener('click', function (e) {
      const btn = e.target.closest('[data-action]');
      if (btn && actions[btn.dataset.action]) actions[btn.dataset.action](btn);
    });
    const search = document.getElementById('mgmt-search-input');
    if (search) search.addEventListener('input', filterMgmtList);
    window.CertainStatsTelemetry.onReady(openFromHash);
  }

  window.CertainStatsAgentManagement = {
    init: init,
    copyCredential: copyCredential,
    showReinstallModal: showReinstallModal,
    startMgmtAgentRename: startMgmtAgentRename,
    cancelMgmtAgentRename: cancelMgmtAgentRename,
    saveMgmtAgentRename: saveMgmtAgentRename
  };

  // Backwards compatibility globals
  window.copyCredential = copyCredential;
  window.showReinstallModal = showReinstallModal;
  window.startMgmtAgentRename = startMgmtAgentRename;
  window.cancelMgmtAgentRename = cancelMgmtAgentRename;
  window.saveMgmtAgentRename = saveMgmtAgentRename;
})();

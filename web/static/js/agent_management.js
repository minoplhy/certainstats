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
    if (readEl) readEl.style.display = 'none';
    if (editEl) editEl.style.display = 'flex';
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
    if (editEl) editEl.style.display = 'none';
    if (readEl) readEl.style.display = 'flex';
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
        const row = document.querySelector('tr[data-mgmt-agent-id="' + agentId + '"]');
        if (row) {
          const nickSpan = row.querySelector('.agent-mgmt-nickname');
          if (nickSpan) nickSpan.textContent = newNickname;
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

  function init(options) {
    options = options || {};
    panelPath = options.panelPath || window.CertainStatsTelemetry.getPanelPath();
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

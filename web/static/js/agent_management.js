(function () {
  'use strict';

  let panelPath = '';

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

  function init(options) {
    options = options || {};
    panelPath = options.panelPath || window.CertainStatsTelemetry.getPanelPath();
  }

  window.CertainStatsAgentManagement = {
    init: init,
    copyCredential: copyCredential,
    showReinstallModal: showReinstallModal
  };

  // Backwards compatibility globals
  window.copyCredential = copyCredential;
  window.showReinstallModal = showReinstallModal;
})();

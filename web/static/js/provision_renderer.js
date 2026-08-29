/* CertainStats — Dynamic Provision & Installation Instruction Renderer */

(function () {
  'use strict';

  const ICONS = {
    copy: '<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="vertical-align: middle;"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path></svg>',
    check: '<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" style="vertical-align: middle;"><polyline points="20 6 9 17 4 12"></polyline></svg>',
    file: '<svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" style="color: var(--accent-primary);"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path><polyline points="14 2 14 8 20 8"></polyline><line x1="16" y1="13" x2="8" y2="13"></line><line x1="16" y1="17" x2="8" y2="17"></line></svg>',
    warning: '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="color: var(--status-offline); flex-shrink: 0; margin-top: 1px;"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"></path><line x1="12" y1="9" x2="12" y2="13"></line><line x1="12" y1="17" x2="12.01" y2="17"></line></svg>'
  };

  function escapeHtml(str) {
    if (!str) return '';
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
  }

  function copyText(text, label, buttonEl) {
    navigator.clipboard.writeText(text).then(() => {
      if (buttonEl) {
        const origHtml = buttonEl.innerHTML;
        buttonEl.innerHTML = ICONS.check + ' Copied';
        buttonEl.classList.add('copied');
        setTimeout(() => {
          buttonEl.innerHTML = origHtml;
          buttonEl.classList.remove('copied');
        }, 2000);
      }
      if (window.CertainStatsTelemetry && window.CertainStatsTelemetry.showToast) {
        window.CertainStatsTelemetry.showToast((label || 'Content') + ' copied to clipboard', true);
      }
    }).catch(() => {
      prompt('Copy ' + (label || 'Content') + ':', text);
    });
  }

  function createInstructionElement(message, key) {
    const wrapper = document.createElement('div');
    wrapper.className = 'instruction-item';
    wrapper.setAttribute('data-key', key);

    // 1. Handle Tabs Container ("tabs")
    if (message.message_type === 'tabs' && Array.isArray(message.children) && message.children.length > 0) {
      const tabsHeader = document.createElement('div');
      tabsHeader.className = 'instruction-tabs-header';

      const tabsPanels = document.createElement('div');
      tabsPanels.className = 'instruction-tabs-panels';

      message.children.forEach((tabChild, tabIdx) => {
        const tabBtn = document.createElement('button');
        tabBtn.type = 'button';
        tabBtn.className = 'instruction-tab-btn' + (tabIdx === 0 ? ' active' : '');
        tabBtn.textContent = tabChild.name || `Option ${tabIdx + 1}`;

        const panel = document.createElement('div');
        panel.className = 'instruction-tab-panel';
        panel.style.display = tabIdx === 0 ? 'flex' : 'none';
        panel.style.flexDirection = 'column';
        panel.style.gap = '16px';

        if (Array.isArray(tabChild.children)) {
          tabChild.children.forEach((subMsg, subIdx) => {
            panel.appendChild(createInstructionElement(subMsg, `${key}-t${tabIdx}-${subIdx}`));
          });
        }

        tabBtn.addEventListener('click', () => {
          Array.from(tabsHeader.children).forEach(btn => btn.classList.remove('active'));
          tabBtn.classList.add('active');
          Array.from(tabsPanels.children).forEach(p => p.style.display = 'none');
          panel.style.display = 'flex';
        });

        tabsHeader.appendChild(tabBtn);
        tabsPanels.appendChild(panel);
      });

      wrapper.appendChild(tabsHeader);
      wrapper.appendChild(tabsPanels);
      return wrapper;
    }

    // 2. Handle Tab Wrapper ("tab")
    if (message.message_type === 'tab') {
      const container = document.createElement('div');
      container.className = 'instruction-tab-content';
      container.style.display = 'flex';
      container.style.flexDirection = 'column';
      container.style.gap = '16px';
      if (Array.isArray(message.children)) {
        message.children.forEach((childMsg, childIdx) => {
          container.appendChild(createInstructionElement(childMsg, `${key}-${childIdx}`));
        });
      }
      return container;
    }

    // 3. Header Label for Leaf Instructions
    if (message.name) {
      const heading = document.createElement('div');
      heading.className = 'instruction-label';
      heading.textContent = message.name;
      wrapper.appendChild(heading);
    }

    // 4. Message Type: "command"
    if (message.message_type === 'command') {
      const cmdBox = document.createElement('div');
      cmdBox.className = 'instruction-command-box';

      const pre = document.createElement('pre');
      pre.className = 'instruction-command-code font-mono';
      pre.textContent = message.content || '';

      const copyBtn = document.createElement('button');
      copyBtn.type = 'button';
      copyBtn.className = 'instruction-copy-btn';
      copyBtn.title = 'Copy Command';
      copyBtn.innerHTML = ICONS.copy + ' Copy';
      copyBtn.addEventListener('click', () => copyText(message.content, message.name || 'Command', copyBtn));

      cmdBox.appendChild(pre);
      cmdBox.appendChild(copyBtn);
      wrapper.appendChild(cmdBox);
      return wrapper;
    }

    // 5. Message Type: "copy"
    if (message.message_type === 'copy') {
      const copyBox = document.createElement('div');
      copyBox.className = 'instruction-copy-badge';

      const code = document.createElement('code');
      code.className = 'instruction-copy-val font-mono';
      code.textContent = message.content || '';

      const copyBtn = document.createElement('button');
      copyBtn.type = 'button';
      copyBtn.className = 'instruction-inline-copy-btn';
      copyBtn.title = 'Copy';
      copyBtn.innerHTML = ICONS.copy + ' Copy';
      copyBtn.addEventListener('click', () => copyText(message.content, message.name || 'Key', copyBtn));

      copyBox.appendChild(code);
      copyBox.appendChild(copyBtn);
      wrapper.appendChild(copyBox);
      return wrapper;
    }

    // 6. Message Type: "big_copy"
    if (message.message_type === 'big_copy') {
      const card = document.createElement('div');
      card.className = 'instruction-big-copy-card';

      const infoGroup = document.createElement('div');
      infoGroup.className = 'big-copy-info';
      infoGroup.innerHTML = `
        <div class="big-copy-icon">${ICONS.file}</div>
        <div class="big-copy-text">
          <div class="big-copy-title">${escapeHtml(message.name || 'Configuration File')}</div>
          <div class="big-copy-desc">${escapeHtml(message.description || 'Configuration content ready for deployment.')}</div>
        </div>
      `;

      const copyBtn = document.createElement('button');
      copyBtn.type = 'button';
      copyBtn.className = 'btn btn-primary btn-sm big-copy-action';
      copyBtn.innerHTML = ICONS.copy + ' Copy Content';
      copyBtn.addEventListener('click', () => copyText(message.content, message.name || 'File content', copyBtn));

      card.appendChild(infoGroup);
      card.appendChild(copyBtn);
      wrapper.appendChild(card);
      return wrapper;
    }

    // 7. Message Type: "note"
    if (message.message_type === 'note') {
      const noteBox = document.createElement('div');
      noteBox.className = 'instruction-note';
      noteBox.innerHTML = message.content || '';
      wrapper.appendChild(noteBox);
      return wrapper;
    }

    // 8. Message Type: "warning"
    if (message.message_type === 'warning') {
      const warnBox = document.createElement('div');
      warnBox.className = 'instruction-warning';
      warnBox.innerHTML = `${ICONS.warning} <div>${message.content || ''}</div>`;
      wrapper.appendChild(warnBox);
      return wrapper;
    }

    return wrapper;
  }

  function renderInstructions(container, messages, parentKey) {
    if (!container) return;
    container.innerHTML = '';
    if (!Array.isArray(messages) || messages.length === 0) {
      container.innerHTML = '<p style="color: var(--text-muted); font-size: 13px;">No instructions available.</p>';
      return;
    }

    const listContainer = document.createElement('div');
    listContainer.className = 'instructions-list';
    listContainer.style.display = 'flex';
    listContainer.style.flexDirection = 'column';
    listContainer.style.gap = '20px';

    messages.forEach((msg, idx) => {
      listContainer.appendChild(createInstructionElement(msg, `${parentKey || 'inst'}-${idx}`));
    });

    container.appendChild(listContainer);
  }

  function loadInstallInstructions(agentId, containerEl) {
    if (!containerEl) return;
    containerEl.innerHTML = `
      <div style="text-align: center; padding: 32px; color: var(--text-muted); font-size: 13px;">
        Fetching installation instructions...
      </div>`;

    const panelPath = window.CertainStatsTelemetry ? window.CertainStatsTelemetry.getPanelPath() : '';
    fetch((panelPath || '') + '/api/agent/install/' + encodeURIComponent(agentId))
      .then(r => r.json().then(data => ({ ok: r.ok, data })))
      .then(({ ok, data }) => {
        if (!ok || !data || !Array.isArray(data.messages)) {
          containerEl.innerHTML = `
            <div class="instruction-warning">
              ${ICONS.warning}
              <div>Failed to load installation instructions: ${escapeHtml((data && data.message) || 'Unknown error')}</div>
            </div>`;
          return;
        }

        renderInstructions(containerEl, data.messages, 'reinstall');
      })
      .catch(() => {
        containerEl.innerHTML = `
          <div class="instruction-warning">
            ${ICONS.warning}
            <div>Network error loading installation instructions.</div>
          </div>`;
      });
  }

  function loadUninstallInstructions(agentId, containerEl) {
    if (!containerEl) return;
    containerEl.innerHTML = `
      <div style="text-align: center; padding: 32px; color: var(--text-muted); font-size: 13px;">
        Fetching uninstall instructions...
      </div>`;

    const panelPath = window.CertainStatsTelemetry ? window.CertainStatsTelemetry.getPanelPath() : '';
    fetch((panelPath || '') + '/api/agent/uninstall/' + encodeURIComponent(agentId))
      .then(r => r.json().then(data => ({ ok: r.ok, data })))
      .then(({ ok, data }) => {
        if (!ok || !data || !Array.isArray(data.messages)) {
          containerEl.innerHTML = `
            <div class="instruction-warning">
              ${ICONS.warning}
              <div>Failed to load uninstall instructions: ${escapeHtml((data && data.message) || 'Unknown error')}</div>
            </div>`;
          return;
        }

        renderInstructions(containerEl, data.messages, 'uninstall');
      })
      .catch(() => {
        containerEl.innerHTML = `
          <div class="instruction-warning">
            ${ICONS.warning}
            <div>Network error loading uninstall instructions.</div>
          </div>`;
      });
  }

  function provisionAgent(type, nickname, onComplete) {
    const panelPath = window.CertainStatsTelemetry ? window.CertainStatsTelemetry.getPanelPath() : '';
    return fetch((panelPath || '') + '/api/agent', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        agent_type: type,
        nickname: nickname
      })
    })
      .then(r => r.json().then(data => ({ ok: r.ok, data })))
      .then(({ ok, data }) => {
        if (!ok) {
          throw new Error((data && data.message) || 'Failed to provision agent');
        }
        if (onComplete && typeof onComplete === 'function') {
          onComplete(data);
        }
        return data;
      });
  }

  let selectedProvisionType = 'beszel';

  function selectProvisionDriver(el, type) {
    selectedProvisionType = type;
    document.querySelectorAll('.driver-select-card').forEach(c => c.classList.remove('selected'));
    if (el) el.classList.add('selected');
  }

  function submitProvisionAgent() {
    const btn = document.getElementById('btn-provision-submit');
    const nickInput = document.getElementById('provision-nickname-input');
    const nickname = nickInput ? nickInput.value.trim() : '';

    if (btn) {
      btn.disabled = true;
      btn.textContent = 'Provisioning...';
    }

    provisionAgent(selectedProvisionType, nickname)
      .then(data => {
        if (btn) {
          btn.disabled = false;
          btn.textContent = 'Provision';
        }

        const stepSelect = document.getElementById('provision-step-select');
        const stepInst = document.getElementById('provision-step-instructions');
        const resTitle = document.getElementById('provision-result-title');
        const container = document.getElementById('provision-instructions-container');

        if (resTitle) resTitle.textContent = 'Agent Provisioned — ' + (data.nickname || data.agent_id);
        if (stepSelect) stepSelect.style.display = 'none';
        if (stepInst) stepInst.style.display = 'block';

        renderInstructions(container, data.messages, 'prov');
      })
      .catch(err => {
        if (btn) {
          btn.disabled = false;
          btn.textContent = 'Provision';
        }
        alert('Failed to provision agent: ' + err.message);
      });
  }

  function finishProvisioning() {
    const modal = document.getElementById('add-agent-modal');
    if (modal) modal.style.display = 'none';
    window.location.reload();
  }

  window.CertainStatsProvisionRenderer = {
    renderInstructions: renderInstructions,
    loadInstallInstructions: loadInstallInstructions,
    loadUninstallInstructions: loadUninstallInstructions,
    provisionAgent: provisionAgent,
    copyText: copyText,
    selectProvisionDriver: selectProvisionDriver,
    submitProvisionAgent: submitProvisionAgent,
    finishProvisioning: finishProvisioning
  };

  // Backwards compatibility globals
  window.selectProvisionDriver = selectProvisionDriver;
  window.submitProvisionAgent = submitProvisionAgent;
  window.finishProvisioning = finishProvisioning;
})();

import { Modal } from './modal.js';

export function openReinstallModal(opts) {
  const modal = document.getElementById('reinstall-modal');
  if (!modal) return;

  const agentId = (typeof opts === 'string') ? opts : (opts.agentId || '');
  const nickname = (typeof opts === 'object' && opts.nickname) ? opts.nickname : agentId;

  const titleEl = document.getElementById('reinstall-modal-title');
  if (titleEl) titleEl.textContent = 'Install ' + nickname;

  const container = document.getElementById('reinstall-instructions-container');
  if (container && window.CertainStatsProvisionRenderer) {
    window.CertainStatsProvisionRenderer.loadInstallInstructions(agentId, container);
  }

  Modal.open(modal);
}

export function openUninstallModal(opts) {
  const modal = document.getElementById('uninstall-modal');
  if (!modal) return;

  const agentId = (typeof opts === 'string') ? opts : (opts.agentId || '');
  const nickname = (typeof opts === 'object' && opts.nickname) ? opts.nickname : agentId;

  const titleEl = document.getElementById('uninstall-modal-title');
  if (titleEl) titleEl.textContent = 'Uninstall ' + nickname;

  const container = document.getElementById('uninstall-instructions-container');
  if (container && window.CertainStatsProvisionRenderer) {
    window.CertainStatsProvisionRenderer.loadUninstallInstructions(agentId, container);
  }

  Modal.open(modal);
}

// Modals are native <dialog class="modal"> elements: Esc and focus trapping come from the browser.
export function initModalBackdropHandlers() {
  document.addEventListener('click', function (e) {
    const opener = e.target.closest('[data-open-modal]');
    if (opener) {
      e.preventDefault();
      Modal.open(opener.getAttribute('data-open-modal'));
      return;
    }
    const closer = e.target.closest('[data-close-modal]');
    if (closer) {
      e.preventDefault();
      Modal.close(closer.getAttribute('data-close-modal') || closer.closest('dialog, .modal-overlay'));
      return;
    }
    // Backdrop click: the event target is the <dialog> itself, outside its content box.
    if (e.target.tagName === 'DIALOG' && e.target.classList.contains('modal')) {
      const r = e.target.getBoundingClientRect();
      const inside = e.clientX >= r.left && e.clientX <= r.right && e.clientY >= r.top && e.clientY <= r.bottom;
      if (!inside) Modal.close(e.target);
    }
  });
}

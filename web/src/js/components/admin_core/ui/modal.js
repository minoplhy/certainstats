export const Modal = {
  open: function (target) {
    const el = typeof target === 'string' ? document.getElementById(target) : target;
    if (!el) return;
    if (el.tagName === 'DIALOG') {
      if (!el.open) el.showModal();
    } else {
      el.style.display = 'block';
    }
  },
  close: function (target) {
    const el = typeof target === 'string' ? document.getElementById(target) : target;
    if (!el) return;
    if (el.tagName === 'DIALOG') {
      if (el.open) el.close();
    } else {
      el.style.display = 'none';
    }
  },
  // Styled replacement for window.confirm(), backed by the shared #confirm-modal partial.
  // opts: { title, message, label, danger, match }. Resolves true only when confirmed.
  confirm: function (opts) {
    opts = opts || {};
    const dlg = document.getElementById('confirm-modal');
    if (!dlg) return Promise.resolve(window.confirm(opts.message || opts.title || 'Are you sure?'));
    const ok = document.getElementById('confirm-modal-ok');
    const matchBox = document.getElementById('confirm-modal-match');
    const matchInput = document.getElementById('confirm-modal-match-input');
    document.getElementById('confirm-modal-title').textContent = opts.title || 'Are you sure?';
    document.getElementById('confirm-modal-message').textContent = opts.message || '';
    document.getElementById('confirm-modal-match-text').textContent = opts.match || '';
    ok.textContent = opts.label || 'Confirm';
    ok.className = 'btn ' + (opts.danger ? 'btn-danger-solid' : 'btn-primary');
    matchBox.hidden = !opts.match;
    matchInput.value = '';
    ok.disabled = !!opts.match;
    matchInput.oninput = function () { ok.disabled = matchInput.value.trim() !== opts.match; };
    dlg.returnValue = '';
    return new Promise(function (resolve) {
      dlg.addEventListener('close', function () { resolve(dlg.returnValue === 'ok'); }, { once: true });
      Modal.open(dlg);
      (opts.match ? matchInput : ok).focus();
    });
  }
};

// Forms with data-confirm ask through Modal.confirm before submitting.
// data-confirm-flash is shown as a toast on the page the submit lands on.
document.addEventListener('submit', function (e) {
  const form = e.target.closest('form[data-confirm]');
  if (!form) return;
  e.preventDefault();
  Modal.confirm({
    title: form.getAttribute('data-confirm-title'),
    message: form.getAttribute('data-confirm'),
    label: form.getAttribute('data-confirm-label'),
    danger: form.hasAttribute('data-confirm-danger'),
    match: form.getAttribute('data-confirm-match')
  }).then(function (confirmed) {
    if (!confirmed) return;
    const flash = form.getAttribute('data-confirm-flash');
    if (flash) {
      try { sessionStorage.setItem('certainstats_flash', flash); } catch (err) {}
    }
    form.submit();
  });
});

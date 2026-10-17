/* First-time setup: the confirmation field reports a mismatch while typing. */
(function () {
  'use strict';
  const pw = document.getElementById('password'), confirm = document.getElementById('confirm_password');
  if (pw && confirm) for (const input of [pw, confirm]) input.addEventListener('input', () => confirm.setCustomValidity(confirm.value && confirm.value !== pw.value ? 'Passwords do not match.' : ''));
})();

import { el, button, api } from './shared.js';

// Machine and bouncer health settings for one collector's CrowdSec type. The
// checks, their levels and their numbers come from the server's registry, so a
// check added there shows up here with no change.

const LEVELS = [['off', 'Off'], ['caution', 'Caution (shown only)'], ['alert', 'Alert (notify)']];

function select(options, value, label) {
  const node = el('select', 'form-select');
  node.setAttribute('aria-label', label);
  for (const [v, text] of options) {
    const option = el('option', '', text);
    option.value = v;
    node.append(option);
  }
  node.value = value;
  return node;
}

function checkRow(check, setting) {
  const row = el('fieldset', 'collector-health-check');
  row.dataset.check = check.name;
  row.append(el('legend', '', check.label));
  const controls = el('div', 'collector-health-controls');
  const level = select(LEVELS, setting.level, check.label + ' level');
  level.dataset.level = '';
  controls.append(level);
  for (const p of check.params) {
    const label = el('label', 'collector-health-param');
    const input = el('input', 'form-input form-input-sm');
    input.type = 'number';
    input.min = p.min;
    input.max = p.max;
    input.required = true;
    input.value = setting.values[p.key] ?? p.default;
    input.dataset.param = p.key;
    label.append(el('span', '', p.label), input);
    if (p.unit) label.append(el('span', 'muted', p.unit));
    controls.append(label);
  }
  row.append(controls, el('p', 'form-hint', check.proves));
  return row;
}

// openHealthForm loads the settings and shows the form inside host. done runs after a save.
export async function openHealthForm(collector, host, done) {
  host.querySelector('.collector-health')?.remove();
  const data = await api('collectors/' + collector.collector_id + '/crowdsec/health');
  const form = el('form', 'collector-health');
  form.noValidate = false;
  form.append(el('p', 'form-hint', "Reads CrowdSec's database, read-only, and only when the collector's host sets CROWDSEC_HEALTH=true. Until then these settings wait."));

  const targetLabel = el('label', 'collector-health-param');
  const target = select([['', 'No notification'], ...data.targets.map(t => [t.target_id, t.name])], data.target_id, 'Notify');
  targetLabel.append(el('span', '', 'Notify'), target);
  form.append(targetLabel);
  if (!data.targets.length) form.append(el('p', 'form-hint', 'Add a notification target under Alerts to be notified.'));

  for (const check of data.registry) form.append(checkRow(check, data.checks[check.name]));

  const message = el('p', 'collector-health-message');
  message.setAttribute('role', 'alert');
  const save = el('button', 'btn btn-primary btn-sm', 'Save');
  save.type = 'submit';
  const actions = el('div', 'collector-health-actions');
  actions.append(save, button('Cancel', 'btn btn-secondary btn-sm', () => form.remove()));
  form.append(message, actions);

  form.addEventListener('submit', async e => {
    e.preventDefault();
    const checks = {};
    for (const row of form.querySelectorAll('[data-check]')) {
      const values = {};
      for (const input of row.querySelectorAll('[data-param]')) values[input.dataset.param] = Number.parseInt(input.value, 10);
      checks[row.dataset.check] = {level: row.querySelector('[data-level]').value, values};
    }
    message.textContent = '';
    save.disabled = true;
    try {
      await api('collectors/' + collector.collector_id + '/crowdsec/health', {method: 'PUT', body: JSON.stringify({target_id: target.value, checks})});
      form.remove();
      done();
    } catch (err) {
      message.textContent = err.message;
      save.disabled = false;
    }
  });
  host.append(form);
  target.focus();
}

import { state } from '../state.js';
import { el, iconButton } from '../format.js';
import { FIELD_LABELS, FIELDS, addFilter, removeFilter } from '../url/state.js';

// renderChips shows each active filter as a removable chip.
export function renderChips(onChange) {
  const ui = state.ui;
  const v = state.view;
  ui.chips.replaceChildren();
  const chip = (field, value, exclude) => {
    const node = el('span', 'cs-chip' + (exclude ? ' is-exclude' : ''));
    node.append(el('span', 'cs-chip-field', FIELD_LABELS[field]), el('span', 'cs-chip-op', exclude ? 'is not' : 'is'), el('span', 'cs-chip-value mono', value));
    const remove = iconButton('exclude', 'Remove filter ' + FIELD_LABELS[field] + (exclude ? ' is not ' : ' is ') + value, () => {
      removeFilter(v, field, value, exclude);
      onChange();
    });
    remove.classList.add('cs-chip-remove');
    node.append(remove);
    return node;
  };
  for (const f of FIELDS) {
    for (const value of v.include[f] || []) ui.chips.append(chip(f, value, false));
    for (const value of v.exclude[f] || []) ui.chips.append(chip(f, value, true));
  }
  if (ui.chips.children.length > 1) {
    const clear = el('button', 'btn btn-ghost btn-sm', 'Clear filters');
    clear.type = 'button';
    clear.addEventListener('click', () => { v.include = {}; v.exclude = {}; v.page = 1; onChange(); });
    ui.chips.append(clear);
  }
  ui.chips.hidden = ui.chips.children.length === 0;
}

// bindFilters wires search, the add-filter form, the time picker and "All".
export function bindFilters(onChange) {
  const ui = state.ui;
  const v = () => state.view;
  ui.search.value = v().q;
  let timer = null;
  ui.search.addEventListener('input', () => {
    clearTimeout(timer);
    timer = setTimeout(() => {
      if (v().q === ui.search.value.trim()) return;
      v().q = ui.search.value.trim();
      v().page = 1;
      onChange();
    }, 350);
  });
  ui.addFilter.addEventListener('submit', event => {
    event.preventDefault();
    const value = ui.filterValue.value.trim();
    if (!value) return;
    addFilter(v(), ui.filterField.value, value, ui.filterOp.value === 'not');
    ui.filterValue.value = '';
    onChange();
  });

  state.picker = window.CertainStatsTelemetry.initCustomTimePicker(ui.timePicker, {
    storageKey: 'certainstats_crowdsec_hours',
    activeHours: v().hours,
    onApply: ({hours, customRange}) => {
      const view = v();
      view.all = false;
      view.hours = hours;
      view.start = customRange ? customRange.start : null;
      view.end = customRange ? customRange.end : null;
      view.page = 1;
      onChange();
    }
  });
  if (v().start && v().end) state.picker.setState({hours: v().hours, customRange: {start: v().start, end: v().end}});
  ui.allTime.addEventListener('click', () => {
    const view = v();
    view.all = !view.all;
    if (!view.all) { view.start = null; view.end = null; }
    view.page = 1;
    onChange();
  });
}

export function syncRangeControls() {
  const ui = state.ui;
  const all = state.view.all;
  ui.allTime.setAttribute('aria-pressed', String(all));
  ui.allTime.classList.toggle('is-active', all);
  ui.timePicker.classList.toggle('is-dimmed', all);
}

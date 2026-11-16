// The "+ Filter" menu and the chips of active filters, shared by collector
// type pages, exposed on CertainStatsTelemetry. Markup: partials/filters/menu.html ("filter_menu", "filter_chips").
// Each page passes its own fields and keeps the filters in its own view.

// Nodes are made in the document of the element they go into.
function el(doc, tag, className, text) {
  const node = doc.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function removeIcon(doc) {
  const ns = 'http://www.w3.org/2000/svg';
  const svg = doc.createElementNS(ns, 'svg');
  svg.setAttribute('viewBox', '0 0 24 24');
  svg.setAttribute('aria-hidden', 'true');
  const path = doc.createElementNS(ns, 'path');
  for (const [k, v] of Object.entries({d: 'M5 12h14', fill: 'none', stroke: 'currentColor', 'stroke-width': '2', 'stroke-linecap': 'round'})) path.setAttribute(k, v);
  svg.append(path);
  return svg;
}

// createFilterMenu wires the menu inside root. fields: [{value, label, placeholder}].
// exclude: false hides the is / is not choice; maxLength caps the value as the API does.
// onAdd(field, value, exclude) runs on submit.
export function createFilterMenu(root, {fields, exclude = true, maxLength = 255, onAdd}) {
  const q = name => root.querySelector('[data-filter-' + name + ']');
  const toggle = q('toggle'), pop = q('popover'), field = q('field'), value = q('value'), match = q('match');
  if (!toggle || !pop || !field || !value) return null;
  const doc = root.ownerDocument;
  field.replaceChildren(...fields.map(f => {
    const o = doc.createElement('option');
    o.value = f.value;
    o.textContent = f.label;
    return o;
  }));
  if (match) match.hidden = !exclude;
  value.maxLength = maxLength;
  const placeholder = () => { value.placeholder = fields.find(f => f.value === field.value)?.placeholder || 'Value'; };
  const setOpen = open => {
    pop.hidden = !open;
    toggle.setAttribute('aria-expanded', String(open));
    if (open) value.focus();
  };
  const closeToToggle = () => { setOpen(false); toggle.focus(); };
  toggle.addEventListener('click', () => setOpen(pop.hidden));
  q('cancel')?.addEventListener('click', closeToToggle);
  pop.addEventListener('keydown', e => {
    if (e.key !== 'Escape') return;
    e.preventDefault();
    closeToToggle();
  });
  doc.addEventListener('click', e => {
    if (!pop.hidden && !pop.contains(e.target) && !toggle.contains(e.target)) setOpen(false);
  });
  field.addEventListener('change', placeholder);
  placeholder();
  pop.addEventListener('submit', e => {
    e.preventDefault();
    const v = value.value.trim();
    if (!v) return;
    const not = exclude && pop.querySelector('input[name="filter-op"]:checked')?.value === 'not';
    value.value = '';
    setOpen(false);
    onAdd(field.value, v, not);
  });
  return {
    open: () => setOpen(true),
    close: () => setOpen(false),
    // setField opens the menu on a field next time, such as the last one used.
    setField(name) {
      if (fields.some(f => f.value === name)) { field.value = name; placeholder(); }
    }
  };
}

// renderFilterChips shows chips ([{label, value, exclude, mono, onRemove}]) in container,
// with "Clear filters" when there is more than one and onClear is given.
export function renderFilterChips(container, chips, {onClear} = {}) {
  if (!container) return;
  const doc = container.ownerDocument;
  container.replaceChildren(...chips.map(c => {
    const op = c.exclude ? 'is not' : 'is';
    const node = el(doc, 'span', 'filter-chip' + (c.exclude ? ' is-exclude' : ''));
    node.append(el(doc, 'span', 'filter-chip-field', c.label), el(doc, 'span', 'filter-chip-op', op), el(doc, 'span', 'filter-chip-value' + (c.mono ? ' mono' : ''), c.value));
    const remove = el(doc, 'button', 'filter-chip-remove');
    remove.type = 'button';
    const label = 'Remove filter ' + c.label + ' ' + op + ' ' + c.value;
    remove.setAttribute('aria-label', label);
    remove.title = label;
    remove.append(removeIcon(doc));
    remove.addEventListener('click', c.onRemove);
    node.append(remove);
    return node;
  }));
  if (chips.length > 1 && onClear) {
    const clear = el(doc, 'button', 'btn btn-ghost btn-sm', 'Clear filters');
    clear.type = 'button';
    clear.addEventListener('click', onClear);
    container.append(clear);
  }
  container.hidden = chips.length === 0;
}

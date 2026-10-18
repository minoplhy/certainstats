// Floating Tooltip for Compact Usage Bars

function el(tag, className, text) {
  const node = document.createElement(tag);
  node.className = className;
  if (text !== undefined) node.textContent = String(text);
  return node;
}

// Rows can carry agent-reported text such as disk paths, so they are set as text, never as HTML.
export function renderTooltip(tooltip, header, rows) {
  const nodes = [el('div', 'tooltip-header', header)];
  rows.forEach(r => {
    const row = el('div', 'tooltip-row');
    const label = el('span', 'tooltip-label');
    const dot = el('span', 'tooltip-dot');
    dot.style.background = String(r.color || '');
    label.append(dot, String(r.label ?? ''));
    row.append(label, el('strong', '', r.val ?? ''));
    nodes.push(row);
  });
  tooltip.replaceChildren(...nodes);
}

export function initFloatingTooltips() {
  let tooltip = document.getElementById('global-floating-tooltip');
  if (!tooltip) {
    tooltip = document.createElement('div');
    tooltip.id = 'global-floating-tooltip';
    document.body.appendChild(tooltip);
  }

  document.addEventListener('mouseover', function (e) {
    const target = e.target.closest('.usage-bar-track');
    if (!target) {
      tooltip.style.display = 'none';
      return;
    }

    const header = target.getAttribute('data-tooltip-header') || 'Metric';
    const rawRows = target.getAttribute('data-tooltip-rows');
    if (!rawRows) return;

    try {
      renderTooltip(tooltip, header, JSON.parse(rawRows));
      tooltip.style.display = 'flex';
    } catch (err) {}
  });

  document.addEventListener('mousemove', function (e) {
    if (tooltip.style.display === 'flex') {
      const x = e.clientX + 15;
      const y = e.clientY;
      tooltip.style.left = (x + 200 > window.innerWidth ? e.clientX - 190 : x) + 'px';
      tooltip.style.top = y + 'px';
    }
  });

  document.addEventListener('mouseout', function (e) {
    const target = e.target.closest('.usage-bar-track');
    if (target) {
      tooltip.style.display = 'none';
    }
  });
}

// Floating Tooltip for Compact Usage Bars
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
      const rows = JSON.parse(rawRows);
      let html = `<div class="tooltip-header">${header}</div>`;
      rows.forEach(r => {
        html += `
          <div class="tooltip-row">
            <span class="tooltip-label"><span class="tooltip-dot" style="background: ${r.color};"></span>${r.label}</span>
            <strong>${r.val}</strong>
          </div>
        `;
      });
      tooltip.innerHTML = html;
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

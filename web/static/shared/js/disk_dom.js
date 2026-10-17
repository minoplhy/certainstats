(function () {
  'use strict';
  // An injective UTF-16 encoding preserves distinct paths, including punctuation.
  function pathId(path) {
    path = path || '/';
    let id = 'p';
    for (let i = 0; i < path.length; i++) id += path.charCodeAt(i).toString(16).padStart(4, '0');
    return id;
  }
  function element(tag, className, text, id) {
    const node = document.createElement(tag);
    node.className = className || '';
    if (text !== undefined) node.textContent = text;
    if (id) node.id = id;
    return node;
  }
  function render(container, paths, prefix, allowed) {
    const fragment = document.createDocumentFragment();
    for (const path of paths) {
      const id = pathId(path);
      for (const kind of ['usage', 'io']) {
        const card = element('div', 'chart-card', undefined, prefix + 'disk-card-' + kind + '-' + id);
        const enabled = metric => !allowed || allowed.includes(metric);
        card.hidden = kind === 'usage' ? !enabled('agent_disk_used') : !enabled('agent_disk_read_bytes') && !enabled('agent_disk_write_bytes');
        const header = element('div', 'chart-header-row');
        const title = element('h3', 'chart-header-title', kind === 'usage' ? 'Disk usage ' : 'Disk I/O ');
        title.append(element('span', 'muted mono', path));
        header.append(title);
        const legend = element('div', 'chart-legend-pills');
        for (const [label, key, color, metric] of kind === 'usage' ? [['Used', 'used', 'sw-s1', 'agent_disk_used']] : [['Read', 'read', 'sw-s2', 'agent_disk_read_bytes'], ['Write', 'write', 'sw-s3', 'agent_disk_write_bytes']]) {
          if (!enabled(metric)) continue;
          const item = element('span', 'chart-legend-item');
          item.append(element('span', 'chart-legend-dot ' + color), document.createTextNode(label + ' '), element('span', 'chart-legend-val', 'Unavailable', prefix + 'live-disk-' + key + '-' + id));
          legend.append(item);
        }
        header.append(legend);
        const canvasContainer = element('div', 'chart-container');
        const canvas = element('canvas', 'chart-canvas', undefined, prefix + 'chart-disk-' + (kind === 'io' ? 'io-' : '') + id);
        canvas.setAttribute('role', 'img');
        canvas.setAttribute('aria-label', (kind === 'usage' ? 'Disk usage' : 'Disk I/O') + ' for ' + path);
        canvasContainer.append(canvas);card.append(header, canvasContainer);fragment.append(card);
      }
    }
    container.replaceChildren(fragment);
  }
  window.CertainStatsDiskDOM = {pathId, render};
})();

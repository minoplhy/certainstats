export function getPanelPath() {
  return (document.body?.getAttribute('data-panel-path') || '').replace(/\/+$/, '');
}

// In-Place HTML5 Path & Hash Router (BASE_PATH/{AGENT_ID})
// reservedSegments are the caller's own route names, which are never treated as an item ID.
export function initRouter(options) {
  const onNavigate = options.onNavigate || function () {};
  const basePath = (options.basePath || '').replace(/\/+$/, '');
  const reserved = options.reservedSegments || [];

  function getTargetId() {
    // 1. Check legacy hash (#/{id} or #{id}) and migrate if present
    if (window.location.hash) {
      const hash = window.location.hash.replace(/^#\/?/, '').trim();
      if (hash) {
        const parts = hash.split('/');
        const targetId = parts[parts.length - 1];
        if (targetId && !reserved.includes(targetId)) {
          const cleanUrl = (basePath || '') + '/' + targetId;
          try {
            history.replaceState({ targetId: targetId }, '', cleanUrl);
          } catch (e) {}
          return targetId;
        }
      }
    }

    // 2. Check pathname relative to basePath
    let path = window.location.pathname;
    if (basePath && path.startsWith(basePath)) {
      path = path.slice(basePath.length);
    }
    path = path.replace(/^\/+|\/+$/g, '');
    if (!path) return null;

    const parts = path.split('/');
    const targetId = parts[parts.length - 1];
    if (targetId && !reserved.includes(targetId)) {
      return targetId;
    }
    return null;
  }

  function handleRoute() {
    const id = getTargetId();
    onNavigate(id);
  }

  window.addEventListener('popstate', handleRoute);
  window.addEventListener('hashchange', handleRoute);
  handleRoute();
}

let globalSocket = null;
let socketStarted = false;
let updateListeners = [];
// Feed lifecycle listeners: {type: 'open' | 'frame' | 'close' | 'dispose', network}. Extras built on
// the shared feed, such as the admin network monitors, follow it through these events.
const feedListeners = new Set();

export function onFeedEvent(listener) {
  feedListeners.add(listener);
  return () => feedListeners.delete(listener);
}

function emit(event) {
  feedListeners.forEach(fn => fn(event));
}

// WebSocket Telemetry Connection
export function initWebSocket(wsPath, onUpdate) {
  if (onUpdate && typeof onUpdate === 'function' && !updateListeners.includes(onUpdate)) {
    updateListeners.push(onUpdate);
  }

  if (socketStarted) {
    return;
  }

  socketStarted = true;
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  let cleanPath = wsPath.replace(/\/+/g, '/');
  if (!cleanPath.startsWith('/')) {
    cleanPath = '/' + cleanPath;
  }
  const fullUrl = (wsPath.startsWith('ws://') || wsPath.startsWith('wss://'))
    ? wsPath
    : protocol + '//' + window.location.host + cleanPath;

  let lastPulse = 0, reconnectTimer = null, disposed = false, failures = 0;
  const protobuf = document.documentElement?.dataset.wsProtobuf === 'true';
  const codec = window.CertainStatsBrowserProtocol;
  function status(text) {
    const node = document.getElementById('feed-status');
    if (!node) return;
    node.hidden = false;
    node.replaceChildren(document.createTextNode(text));
    if (failures >= 3) {
      const reload = document.createElement('button');
      reload.type = 'button';
      reload.textContent = 'Reload page';
      reload.addEventListener('click', () => window.location.reload());
      node.append(document.createTextNode(' '), reload);
    }
  }
  const staleTimer = setInterval(() => {
    if (lastPulse && Date.now() - lastPulse > 45000) {
      status('Live feed stale · Last pulse ' + new Date(lastPulse).toLocaleTimeString());
    }
  }, 5000);
  window.addEventListener('pagehide', () => {
    disposed = true;
    clearInterval(staleTimer);
    clearTimeout(reconnectTimer);
    globalSocket?.close();
    emit({type: 'dispose'});
    feedListeners.clear();
    updateListeners = [];
  }, {once: true});

  function connect() {
    if (disposed) return;
    status('Connecting to live feed…');
    try {
      if (protobuf && !codec) throw new Error('Telemetry decoder unavailable. Reload the page.');
      const socket = protobuf ? new WebSocket(fullUrl, codec.protocol) : new WebSocket(fullUrl);
      globalSocket = socket;
      if (protobuf) socket.binaryType = 'arraybuffer';
      let opened = false;
      socket.onopen = function () {
        if (disposed) { socket.close(); return; }
        opened = true;
        failures = 0;
        emit({type: 'open'});
        window.dispatchEvent(new Event('certainstats_feed_connected'));
        status('Connected · Waiting for telemetry');
      };
      socket.onmessage = function (event) {
        if (disposed || socket !== globalSocket) return;
        let snaps, network;
        try {
          if (protobuf) {
            const envelope = codec.decode(event.data);
            snaps = codec.snapshots(envelope);
            network = codec.network(envelope);
          } else {
            if (typeof event.data !== 'string') throw new Error('Expected JSON text frame');
            const message = JSON.parse(event.data);
            if (message.type !== 'agent_update' || !message.data || typeof message.data !== 'object' || Array.isArray(message.data)) throw new Error('Invalid telemetry pulse');
            snaps = message.data;
            network = message.network;
            if (network !== undefined && (!network || typeof network !== 'object' || Array.isArray(network))) throw new Error('Invalid network pulse');
          }
        } catch (e) {
          console.error('[CertainStats WS] Invalid telemetry frame:', e);
          socket.close(1002, 'Invalid telemetry frame');
          return;
        }
        lastPulse = Date.now();
        status('Live · Last update ' + new Date(lastPulse).toLocaleTimeString() + ' · ' + Intl.DateTimeFormat().resolvedOptions().timeZone);
        updateListeners.forEach(fn => fn(snaps));
        emit({type: 'frame', network});
      };
      socket.onerror = function (err) {
        if (!disposed) console.warn('[CertainStats WS] Error:', err);
      };
      socket.onclose = function () {
        if (disposed || socket !== globalSocket) return;
        emit({type: 'close'});
        if (!opened) failures++;
        status('Disconnected · Retrying…');
        reconnectTimer = setTimeout(connect, 3000);
      };
    } catch (e) {
      failures++;
      console.error('[CertainStats WS] Connect exception:', e);
      status('Connection failed · Retrying…');
      reconnectTimer = setTimeout(connect, 5000);
    }
  }

  connect();
}

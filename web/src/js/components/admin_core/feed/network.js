// Network pulses ride the shared agent feed; undefined until the first one arrives.
let latestNetwork;
let lastNetworkPulse = 0;
let networkConnected = false;
// Counts socket connections so views can reconcile after a reconnect.
let connectionGeneration = 0;
const networkListeners = new Set();

// Follows the shared telemetry feed; only the admin panel's feed carries network pulses.
export function trackNetworkFeed(onFeedEvent) {
  onFeedEvent(event => {
    if (event.type === 'open') {
      networkConnected = true;
      connectionGeneration++;
    } else if (event.type === 'close') {
      networkConnected = false;
    } else if (event.type === 'dispose') {
      networkConnected = false;
      networkListeners.clear();
    } else if (event.type === 'frame' && event.network !== undefined) {
      latestNetwork = event.network;
      lastNetworkPulse = Date.now();
      networkListeners.forEach(fn => fn(event.network));
    }
  });
}

export function subscribeNetwork(listener) {
  networkListeners.add(listener);
  if (latestNetwork !== undefined) listener(latestNetwork);
  return () => networkListeners.delete(listener);
}

export function getNetworkFeedState() {
  return {connected: networkConnected, lastPulse: lastNetworkPulse, generation: connectionGeneration};
}

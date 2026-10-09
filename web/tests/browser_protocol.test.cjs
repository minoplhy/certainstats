const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const fixtures = require('./fixtures/browser_protocol.json');
const protocolSource = fs.readFileSync('web/static/js/browser_protocol.js', 'utf8');

function codecContext(extra = {}) {
  const context = {ArrayBuffer, Uint8Array, TextDecoder, ...extra};
  context.window = context;
  vm.runInNewContext(protocolSource, context);
  return context;
}
const codec = codecContext().CertainStatsBrowserProtocol;
test('decoder never accesses WebAssembly under the strict browser CSP', () => {
  let attempts = 0;
  const WebAssembly = new Proxy({}, {get() {attempts++; throw new Error('Blocked by CSP');}});
  const decoder = codecContext({WebAssembly}).CertainStatsBrowserProtocol;
  for (const fixture of fixtures) {
    const decoded = decoder.decode(Uint8Array.from(Buffer.from(fixture.payload, 'base64')));
    assert.deepEqual(JSON.parse(JSON.stringify(decoder.snapshots(decoded))), fixture.expected);
  }
  assert.equal(attempts, 0);
});
for (const fixture of fixtures) {
  test('Go wire fixture decodes in the shipped browser: ' + fixture.name, () => {
    const decoded = codec.decode(Uint8Array.from(Buffer.from(fixture.payload, 'base64')));
    const snapshots = codec.snapshots(decoded);
    assert.deepEqual(JSON.parse(JSON.stringify(snapshots)), fixture.expected);
  });
}
test('uint64 values stay exact until converted for the existing UI', () => {
  const full = fixtures.find(f => f.name === 'full');
  const envelope = codec.decode(Uint8Array.from(Buffer.from(full.payload, 'base64')));
  assert.equal(envelope.pulse.agents['private-agent'].disk_total_bytes.value.toString(), '18446744073709551615');
  assert.equal(envelope.pulse.agents['private-agent'].metadata.ram_size.toString(), '18446744073709551615');
});
test('public decoded objects distinguish denied, unavailable, zero, and false', () => {
  const decodeFixture = name => codec.snapshots(codec.decode(Uint8Array.from(Buffer.from(fixtures.find(f => f.name === name).payload, 'base64'))))['public-agent'];
  const publicSnapshot = decodeFixture('public');
  assert.equal(publicSnapshot.cpu_usage_percent, 0);
  assert.equal(publicSnapshot.is_online, false);
  assert.equal(Object.hasOwn(publicSnapshot, 'agent_id'), false);
  assert.equal(Object.hasOwn(publicSnapshot, 'metadata'), false);
  assert.equal(Object.hasOwn(publicSnapshot, 'ram_used_bytes'), false);
  assert.equal(Object.hasOwn(publicSnapshot.disks[0], 'total_bytes'), false);
  assert.equal(Object.hasOwn(publicSnapshot.disks[0], 'read_bytes'), false);
  assert.equal(decodeFixture('missing').cpu_usage_percent, null);
});
test('decoder rejects text, absent pulses and truncation; accepts additive unknown fields', () => {
  assert.throws(() => codec.decode('{"type":"agent_update"}'));
  assert.throws(() => codec.decode(new Uint8Array()));
  assert.throws(() => codec.decode(Uint8Array.from([0x0a, 0x7f])));
  const empty = Buffer.from(fixtures.find(f => f.name === 'empty').payload, 'base64');
  const extended = Buffer.concat([empty, Buffer.from([0x98, 0x06, 0x01])]);
  assert.deepEqual(Object.keys(codec.snapshots(codec.decode(Uint8Array.from(extended)))), []);
});

function feed(protobuf = true) {
  const timers = new Map(), intervals = new Map(), events = {}, sockets = [];
  let sequence = 0, reloads = 0, now = Date.now();
  class Clock extends Date {static now() {return now;}}
  class Node {
    constructor(text = '') {this.textContent = text; this.children = []; this.events = {};}
    replaceChildren(...children) {this.children = children;}
    append(...children) {this.children.push(...children);}
    addEventListener(event, callback) {this.events[event] = callback;}
  }
  const status = new Node();
  class Socket {
    static OPEN = 1;
    constructor(url, protocol) {this.url = url; this.protocol = protocol; this.readyState = 0; sockets.push(this);}
    open() {this.readyState = 1; this.onopen();}
    close(code, reason) {this.closeCode = code; this.closeReason = reason; this.readyState = 3; this.onclose?.();}
    message(data) {this.onmessage({data});}
  }
  const context = codecContext({
    document: {documentElement: {dataset: {wsProtobuf: String(protobuf)}}, readyState: 'loading', addEventListener: () => {}, getElementById: id => id === 'feed-status' ? status : null,
      createTextNode: text => new Node(text), createElement: () => new Node()},
    location: {protocol: 'https:', host: 'example.test', reload: () => {reloads++;}},
    addEventListener: (event, callback) => {events[event] = callback;},
    setInterval: callback => {const id = ++sequence; intervals.set(id, callback); return id;},
    clearInterval: id => intervals.delete(id),
    setTimeout: callback => {const id = ++sequence; timers.set(id, callback); return id;},
    clearTimeout: id => timers.delete(id),
    console: {warn() {}, error() {}, log() {}},
    WebSocket: Socket, Intl, Date: Clock, Event: class {constructor(type) {this.type = type;}}, dispatchEvent(event) {events[event.type]?.();}
  });
  vm.runInNewContext(fs.readFileSync('web/static/js/telemetry.js', 'utf8'), context);
  return {context, sockets, timers, intervals, events, status, reloads: () => reloads,
    advance(ms) {now += ms; intervals.forEach(callback => callback());},
    retry() {const [id, callback] = timers.entries().next().value; timers.delete(id); callback();}};
}
test('feed requests binary Protobuf and delivers full and empty pulses', () => {
  const f = feed(), received = [];
  f.context.CertainStatsTelemetry.initWebSocket('/panel/api/ws', snaps => received.push(snaps));
  const socket = f.sockets[0];
  assert.equal(socket.protocol, 'certainstats.protobuf.v1');
  assert.equal(socket.binaryType, 'arraybuffer');
  socket.open();
  for (const name of ['public', 'empty']) {
    const payload = Uint8Array.from(Buffer.from(fixtures.find(f => f.name === name).payload, 'base64'));
    socket.message(payload.buffer);
  }
  assert.equal(received.length, 2);
  assert.equal(received[0]['public-agent'].cpu_usage_percent, 0);
  assert.deepEqual(Object.keys(received[1]), []);
  assert.match(f.status.children[0].textContent, /^Live · Last update/);
});
test('invalid frames do not update freshness; page disposal prevents reconnects', () => {
  const f = feed(), received = [];
  f.context.CertainStatsTelemetry.initWebSocket('/dashboard/api/ws/id', snaps => received.push(snaps));
  f.sockets[0].open();
  f.sockets[0].message(Uint8Array.from([0x0a, 0x7f]).buffer);
  assert.equal(received.length, 0);
  assert.equal(f.sockets[0].closeCode, 1002);
  assert.match(f.status.children[0].textContent, /^Disconnected/);
  assert.equal(f.timers.size, 1);
  f.events.pagehide();
  assert.equal(f.timers.size, 0);
  assert.equal(f.intervals.size, 0);
});
for (const protobuf of [false, true]) test(`three failed handshakes offer reload; opening clears that state (${protobuf})`, () => {
  const f = feed(protobuf);
  f.context.CertainStatsTelemetry.initWebSocket('/api/ws', () => {});
  for (let i = 0; i < 3; i++) {f.sockets[i].close(); if (i < 2) f.retry();}
  const reload = f.status.children.find(node => node.textContent === 'Reload page');
  assert.ok(reload);
  reload.events.click();
  assert.equal(f.reloads(), 1);
  f.retry();
  f.sockets[3].open();
  assert.equal(f.status.children.length, 1);
});

for (const protobuf of [false, true]) {
  test(`feed lifecycle in ${protobuf ? 'Protobuf' : 'JSON'} mode`, () => {
    const f = feed(protobuf), received = [];
    if (!protobuf) delete f.context.CertainStatsBrowserProtocol;
    f.context.CertainStatsTelemetry.initWebSocket('/api/ws', s => received.push(s));
    const socket = f.sockets[0];
    assert.equal(socket.protocol, protobuf ? 'certainstats.protobuf.v1' : undefined);
    socket.open();
    const frame = protobuf ? Uint8Array.from(Buffer.from(fixtures.find(f => f.name === 'empty').payload, 'base64')).buffer : JSON.stringify({type:'agent_update', data:{}});
    socket.message(frame);
    assert.equal(received.length, 1);
    f.advance(46000);
    assert.match(f.status.children[0].textContent, /^Live feed stale/);
    socket.message(frame);
    assert.match(f.status.children[0].textContent, /^Live ·/);
    socket.message(protobuf ? 'wrong format' : '{bad json');
    assert.equal(socket.closeCode, 1002);
    assert.equal(received.length, 2);
    assert.equal(f.timers.size, 1);
    f.retry();
    assert.equal(f.sockets.length, 2);
    f.events.pagehide();
    assert.equal(f.timers.size, 0);
    assert.equal(f.intervals.size, 0);
  });
}

const networkFixture = require('./fixtures/network_protocol.json');
test('network wire normalizes identically to owner JSON; omission differs from empty', () => {
  const envelope = codec.decode(Uint8Array.from(Buffer.from(networkFixture.payload, 'base64')));
  assert.deepEqual(JSON.parse(JSON.stringify(codec.network(envelope))), networkFixture.expected);
  assert.equal(codec.network(codec.decode(Uint8Array.from(Buffer.from(fixtures.find(f => f.name === 'empty').payload, 'base64')))), undefined);
});
for (const protobuf of [false, true]) test(`shared read-only connection delivers network and agent data (${protobuf})`, () => {
  const f = feed(protobuf), agents = [], networks = [];
  const telemetry = f.context.CertainStatsTelemetry;
  // Calling from two views while CONNECTING must still open exactly one socket.
  telemetry.initWebSocket('/panel/api/ws', value => agents.push(value));
  telemetry.initWebSocket('/panel/api/ws');
  assert.equal(f.sockets.length, 1);
  const unsubscribe = telemetry.subscribeNetwork(value => networks.push(value));
  const socket = f.sockets[0];
  socket.send = () => {throw new Error('read-only feed sent an application message');};
  socket.open();
  socket.message(protobuf ? Uint8Array.from(Buffer.from(networkFixture.payload, 'base64')).buffer : JSON.stringify({type:'agent_update',data:{node:{available:true}},network:networkFixture.expected}));
  assert.equal(agents.length, 1);
  assert.equal(networks.length, 1);
  assert.deepEqual(JSON.parse(JSON.stringify(networks[0])), networkFixture.expected);
  const replay = [];
  const disposeReplay = telemetry.subscribeNetwork(value => replay.push(value));
  assert.equal(replay.length, 1);
  assert.equal(telemetry.getNetworkFeedState().connected, true);
  unsubscribe(); disposeReplay();
  socket.close();
  assert.equal(telemetry.getNetworkFeedState().connected, false);
  f.retry();
  assert.equal(f.sockets.length, 2);
  telemetry.initWebSocket('/panel/api/ws');
  assert.equal(f.sockets.length, 2);
  f.sockets[1].open();
  assert.equal(telemetry.getNetworkFeedState().generation, 2);
  f.events.pagehide();
});
test('network JSON omission preserves the last pulse; empty replaces it', () => {
  const f = feed(false), telemetry = f.context.CertainStatsTelemetry, received=[];
  telemetry.initWebSocket('/api/ws');telemetry.subscribeNetwork(value=>received.push(value));
  const socket=f.sockets[0];socket.open();
  socket.message(JSON.stringify({type:'agent_update',data:{},network:{}}));
  const at=telemetry.getNetworkFeedState().lastPulse;
  f.advance(10000);
  socket.message(JSON.stringify({type:'agent_update',data:{}}));
  assert.equal(received.length,1);assert.equal(telemetry.getNetworkFeedState().lastPulse,at);
  f.events.pagehide();
});

'use strict';

/*
 * Metadata-first Frida capture for the owner's normal Motorola Nursery traffic.
 * It never writes unredacted text or packet hex. Payload bytes are summarized
 * as redacted printable text so account credentials and tokens do not land in
 * debug files by default.
 */

const MAX_CAPTURE = 8192;
const fdPeers = {};
const getpeernamePtr = Module.findGlobalExportByName('getpeername');
const sslGetFdPtr = Module.findGlobalExportByName('SSL_get_fd');
const getpeernameFn = getpeernamePtr ? new NativeFunction(getpeernamePtr, 'int', ['int', 'pointer', 'pointer']) : null;
const sslGetFdFn = sslGetFdPtr ? new NativeFunction(sslGetFdPtr, 'int', ['pointer']) : null;

function now() { return new Date().toISOString(); }
function emit(event, data) {
  const row = Object.assign({ ts: now(), event: event }, data || {});
  console.log('[VM65CAP] ' + JSON.stringify(row));
}

function redact(text) {
  if (!text) return text;
  let s = String(text).replace(/[\r\n\t]+/g, function (m) {
    return m.replace(/\r/g, '\\r').replace(/\n/g, '\\n').replace(/\t/g, '\\t');
  });
  s = s.replace(/[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}/gi, '<email>');
  s = s.replace(/(password|passwd|accessToken|sessionToken|masterToken|deviceToken|token)([=: ]+)([^ &\\]+)/gi, '$1$2<redacted>');
  s = s.replace(/(v3_loginset\s+\d+\s+\S+\s+email\s+\S+\s+)\S+/gi, '$1<redacted>');
  s = s.replace(/([?&]accessToken=)[^ &\\]+/gi, '$1<redacted>');
  // Long opaque values are protocol fields but unsafe as raw log material.
  s = s.replace(/\b[A-Za-z0-9_-]{24,}\b/g, function (v) {
    if (/^(primary|vrelay|motorola)/i.test(v)) return v;
    return '<opaque:' + v.length + '>';
  });
  return s;
}

function preview(ptr, length) {
  const count = Math.min(Number(length), MAX_CAPTURE);
  if (count <= 0 || ptr.isNull()) return '';
  try {
    const bytes = new Uint8Array(ptr.readByteArray(count));
    let out = '';
    for (let i = 0; i < bytes.length; i++) {
      const b = bytes[i];
      out += (b >= 0x20 && b <= 0x7e) || b === 10 || b === 13 || b === 9
        ? String.fromCharCode(b) : '.';
    }
    return redact(out);
  } catch (_) { return '<unreadable>'; }
}

function ipv4(sockaddr) {
  try {
    if (sockaddr.isNull() || sockaddr.add(0).readU16() !== 2) return null;
    const portBE = sockaddr.add(2).readU16();
    const port = ((portBE & 0xff) << 8) | ((portBE >>> 8) & 0xff);
    const a = sockaddr.add(4).readU8(), b = sockaddr.add(5).readU8();
    const c = sockaddr.add(6).readU8(), d = sockaddr.add(7).readU8();
    return a + '.' + b + '.' + c + '.' + d + ':' + port;
  } catch (_) { return null; }
}

function peerForFd(fd) {
  if (fdPeers[fd]) return fdPeers[fd];
  if (!getpeernameFn) return null;
  try {
    const address = Memory.alloc(128), length = Memory.alloc(4);
    length.writeU32(128);
    if (getpeernameFn(fd, address, length) !== 0) return null;
    const peer = ipv4(address);
    if (peer) fdPeers[fd] = peer;
    return peer;
  } catch (_) { return null; }
}

function attachExport(name, callbacks) {
  const seen = {};
  Process.enumerateModules().forEach(function (module) {
    let address = null;
    try { address = module.findExportByName(name); } catch (_) {}
    if (!address || seen[address.toString()]) return;
    seen[address.toString()] = true;
    try {
      Interceptor.attach(address, callbacks(module.name));
      emit('hook', { symbol: name, module: module.name, address: address.toString() });
    } catch (e) { emit('hook-error', { symbol: name, module: module.name, error: String(e) }); }
  });
}

attachExport('connect', function (module) { return {
  onEnter(args) { this.fd = args[0].toInt32(); this.peer = ipv4(args[1]); },
  onLeave(retval) {
    if (this.peer) fdPeers[this.fd] = this.peer;
    emit('connect', { module: module, fd: this.fd, peer: this.peer, result: retval.toInt32() });
  }
}; });

['send', 'write'].forEach(function (name) {
  attachExport(name, function (module) { return { onEnter(args) {
    const fd = args[0].toInt32(), length = Number(args[2]);
    const peer = peerForFd(fd);
    if (!peer) return;
    emit('io', { direction: 'out', api: name, module: module, fd: fd,
      peer: peer, length: length, preview: preview(args[1], length) });
  }}; });
});

['recv', 'read'].forEach(function (name) {
  attachExport(name, function (module) { return {
    onEnter(args) { this.fd = args[0].toInt32(); this.buf = args[1]; },
    onLeave(retval) {
      const length = retval.toInt32();
      const peer = peerForFd(this.fd);
      if (!peer) return;
      if (length > 0) emit('io', { direction: 'in', api: name, module: module,
        fd: this.fd, peer: peer, length: length,
        preview: preview(this.buf, length) });
    }
  }; });
});

['SSL_write', 'SSL_read'].forEach(function (name) {
  attachExport(name, function (module) { return name === 'SSL_write' ? {
    onEnter(args) {
      const fd = sslGetFdFn ? sslGetFdFn(args[0]) : -1;
      const peer = fd >= 0 ? peerForFd(fd) : null;
      if (!peer) return;
      const length = args[2].toInt32();
      emit('tls-plaintext', { direction: 'out', api: name, module: module,
        fd: fd, peer: peer, length: length, preview: preview(args[1], length) });
    }
  } : {
    onEnter(args) {
      this.fd = sslGetFdFn ? sslGetFdFn(args[0]) : -1;
      this.peer = this.fd >= 0 ? peerForFd(this.fd) : null;
      this.buf = args[1];
    },
    onLeave(retval) {
      const length = retval.toInt32();
      if (!this.peer) return;
      if (length > 0) emit('tls-plaintext', { direction: 'in', api: name,
        module: module, fd: this.fd, peer: this.peer, length: length,
        preview: preview(this.buf, length) });
    }
  }; });
});

emit('ready', { arch: Process.arch, pid: Process.id, maxPreview: MAX_CAPTURE,
  policy: 'redacted-text-only; no raw hex' });

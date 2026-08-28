'use strict';

/*
 * ARM BoringSSL plaintext dump for the 5GenCare control flow.
 *
 * Run on the native arm64-v8a AVD (the app's armeabi-v7a libs run natively, so
 * libflutter.so is finally visible to Frida). Hooks SSL_read/SSL_write wherever
 * they resolve by name — Flutter's Dart runtime BoringSSL lives in libflutter.so;
 * conscrypt's libssl.so carries only Google/Firebase and is kept for contrast.
 *
 * For every record it reports direction, owning module, the socket fd and its
 * peer address (to separate the 5GenCare control socket from Google traffic),
 * SNI and a redacted printable preview. It never writes secrets in the clear.
 *
 * If SSL_read/SSL_write are stripped from libflutter.so, this reports zero
 * flutter hooks — then fall back to a BoringSSL signature scan or a proxy MITM
 * with the Flutter pinning bypass (see docs/arm-5gencare-capture.md).
 */

var MAX = 8192;
function now() { return new Date().toISOString(); }
function emit(o) { console.log('[BSSL] ' + JSON.stringify(Object.assign({ ts: now() }, o))); }

function redact(text) {
  var s = String(text).replace(/[\r\n\t]/g, function (m) { return m === '\r' ? '\\r' : m === '\n' ? '\\n' : '\\t'; });
  s = s.replace(/[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}/gi, '<email>');
  s = s.replace(/(password|passwd|accessToken|sessionToken|masterToken|deviceToken|token|pwd|sid)([=:"\s]+)([^\s&"\\]+)/gi, '$1$2<redacted>');
  s = s.replace(/\b[A-Za-z0-9_-]{28,}\b/g, function (v) { return '<opaque:' + v.length + '>'; });
  return s;
}

function preview(ptr, len) {
  var count = Math.min(Number(len), MAX);
  if (count <= 0 || ptr.isNull()) return '';
  try {
    var bytes = new Uint8Array(ptr.readByteArray(count));
    var out = '';
    for (var i = 0; i < bytes.length; i++) {
      var b = bytes[i];
      out += (b >= 0x20 && b <= 0x7e) ? String.fromCharCode(b) : '.';
    }
    return redact(out);
  } catch (e) { return '<unreadable>'; }
}

// --- fd + peer resolution, to separate the 5GenCare socket from Google ---
var getFd = null, getServername = null;
var p;
if ((p = Module.findGlobalExportByName('SSL_get_fd'))) getFd = new NativeFunction(p, 'int', ['pointer']);
if ((p = Module.findGlobalExportByName('SSL_get_servername'))) getServername = new NativeFunction(p, 'pointer', ['pointer', 'int']);

function sni(ssl) {
  if (!getServername) return null;
  try { var q = getServername(ssl, 0); return q.isNull() ? null : q.readCString(); } catch (e) { return null; }
}

// getpeername(fd) -> "ip:port" for IPv4/IPv6, best-effort.
var getpeername = null;
if ((p = Module.findGlobalExportByName('getpeername'))) {
  getpeername = new NativeFunction(p, 'int', ['int', 'pointer', 'pointer']);
}
function peer(ssl) {
  if (!getFd || !getpeername) return null;
  var fd;
  try { fd = getFd(ssl); } catch (e) { return null; }
  if (fd < 0) return null;
  try {
    var addr = Memory.alloc(28);
    var lenp = Memory.alloc(4); lenp.writeU32(28);
    if (getpeername(fd, addr, lenp) !== 0) return { fd: fd };
    var fam = addr.readU16();
    if (fam === 2) { // AF_INET
      var port = (addr.add(2).readU8() << 8) | addr.add(3).readU8();
      var ip = addr.add(4).readU8() + '.' + addr.add(5).readU8() + '.' + addr.add(6).readU8() + '.' + addr.add(7).readU8();
      return { fd: fd, peer: ip + ':' + port };
    }
    return { fd: fd, family: fam };
  } catch (e) { return { fd: fd }; }
}

function hookIn(moduleName, addr, name) {
  if (name === 'SSL_write') {
    Interceptor.attach(addr, { onEnter: function (args) {
      var pr = peer(args[0]);
      emit({ event: 'tls', dir: 'out', module: moduleName, sni: sni(args[0]),
             fd: pr && pr.fd, peer: pr && pr.peer, len: args[2].toInt32(),
             preview: preview(args[1], args[2].toInt32()) });
    }});
  } else {
    Interceptor.attach(addr, {
      onEnter: function (args) { this.ssl = args[0]; this.buf = args[1]; },
      onLeave: function (ret) {
        var n = ret.toInt32(); if (n <= 0) return;
        var pr = peer(this.ssl);
        emit({ event: 'tls', dir: 'in', module: moduleName, sni: sni(this.ssl),
               fd: pr && pr.fd, peer: pr && pr.peer, len: n, preview: preview(this.buf, n) });
      }
    });
  }
}

var hooks = 0, flutterHooks = 0;
var seen = {};
Process.enumerateModules().forEach(function (m) {
  ['SSL_write', 'SSL_read'].forEach(function (name) {
    var addr = null;
    try { addr = m.findExportByName(name); } catch (e) {}
    if (!addr) return;
    var key = name + '@' + addr.toString();
    if (seen[key]) return;
    seen[key] = true;
    try {
      hookIn(m.name, addr, name); hooks++;
      if (m.name === 'libflutter.so') flutterHooks++;
      emit({ event: 'hook', module: m.name, symbol: name, address: addr.toString() });
    } catch (e) { emit({ event: 'hook-error', module: m.name, symbol: name, error: String(e) }); }
  });
});

emit({ event: 'ready', hooks: hooks, flutterHooks: flutterHooks, arch: Process.arch });
if (flutterHooks === 0) {
  emit({ event: 'note',
         message: 'No SSL_read/SSL_write export in libflutter.so — symbols are stripped. ' +
                  'Fall back to a BoringSSL signature scan or a proxy MITM with the Flutter ' +
                  'pinning bypass. See docs/arm-5gencare-capture.md.' });
}

'use strict';

/*
 * Direct BoringSSL plaintext dump for conscrypt libssl.so, with no fd/peer
 * gating (that gating silently dropped everything in the earlier capture).
 * Purpose: determine whether the 5GenCare control flow traverses the x86
 * conscrypt TLS stack (hookable) or Flutter's ARM BoringSSL (invisible here).
 * Redacts obvious secrets; caps preview length.
 */

const MAX = 4096;
function now() { return new Date().toISOString(); }
function emit(o) { console.log('[TLSDUMP] ' + JSON.stringify(Object.assign({ ts: now() }, o))); }

function redact(text) {
  let s = String(text).replace(/[\r\n\t]/g, function (m) { return m === '\r' ? '\\r' : m === '\n' ? '\\n' : '\\t'; });
  s = s.replace(/[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}/gi, '<email>');
  s = s.replace(/(password|passwd|accessToken|sessionToken|masterToken|deviceToken|token|pwd)([=: "]+)([^ &"\\]+)/gi, '$1$2<redacted>');
  s = s.replace(/\b[A-Za-z0-9_-]{28,}\b/g, function (v) { return '<opaque:' + v.length + '>'; });
  return s;
}

function preview(ptr, len) {
  const count = Math.min(Number(len), MAX);
  if (count <= 0 || ptr.isNull()) return '';
  try {
    const bytes = new Uint8Array(ptr.readByteArray(count));
    let out = '';
    for (let i = 0; i < bytes.length; i++) {
      const b = bytes[i];
      out += (b >= 0x20 && b <= 0x7e) ? String.fromCharCode(b) : '.';
    }
    return redact(out);
  } catch (_) { return '<unreadable>'; }
}

// SSL_get_servername(ssl, TLSEXT_NAMETYPE_host_name=0) -> const char*
let getServername = null;
const snPtr = Module.findGlobalExportByName('SSL_get_servername');
if (snPtr) getServername = new NativeFunction(snPtr, 'pointer', ['pointer', 'int']);
function sni(ssl) {
  if (!getServername) return null;
  try { const p = getServername(ssl, 0); return p.isNull() ? null : p.readCString(); } catch (_) { return null; }
}

function hookIn(moduleName, addr, name) {
  if (name === 'SSL_write') {
    Interceptor.attach(addr, { onEnter(args) {
      emit({ event: 'tls', dir: 'out', module: moduleName, sni: sni(args[0]),
        len: args[2].toInt32(), preview: preview(args[1], args[2].toInt32()) });
    }});
  } else {
    Interceptor.attach(addr, {
      onEnter(args) { this.ssl = args[0]; this.buf = args[1]; },
      onLeave(ret) { const n = ret.toInt32(); if (n > 0)
        emit({ event: 'tls', dir: 'in', module: moduleName, sni: sni(this.ssl),
          len: n, preview: preview(this.buf, n) }); }
    });
  }
}

let hooks = 0;
const seen = {};
Process.enumerateModules().forEach(function (m) {
  ['SSL_write', 'SSL_read'].forEach(function (name) {
    let addr = null;
    try { addr = m.findExportByName(name); } catch (_) {}
    if (!addr) return;
    const key = name + '@' + addr.toString();
    if (seen[key]) return; // dedup: libjavacrypto forwards to the same libssl address
    seen[key] = true;
    try { hookIn(m.name, addr, name); hooks++; emit({ event: 'hook', module: m.name, symbol: name, address: addr.toString() }); }
    catch (e) { emit({ event: 'hook-error', module: m.name, symbol: name, error: String(e) }); }
  });
});
emit({ event: 'ready', hooks: hooks, arch: Process.arch });

'use strict';

/*
 * Diagnostic probe: locate the TLS stack that carries the 5GenCare control
 * flow so a keylog hook can be aimed correctly. Prints modules of interest and
 * which BoringSSL/OpenSSL symbols each exports. Read-only; no payloads.
 */

function emit(o) { console.log('[SSLPROBE] ' + JSON.stringify(o)); }

const wantedSyms = [
  'SSL_read', 'SSL_write',
  'SSL_new', 'SSL_CTX_new',
  'SSL_CTX_set_keylog_callback', 'SSL_CTX_set_info_callback',
  'SSL_get_session', 'SSL_SESSION_get_master_key', 'SSL_get_client_random',
  'SSL_do_handshake', 'SSL_connect'
];

const modules = Process.enumerateModules();
emit({ event: 'summary', arch: Process.arch, pid: Process.id, moduleCount: modules.length });

const interesting = /ssl|crypto|flutter|libapp|devconn|boringssl|conscrypt|cronet|okhttp|netty/i;

modules.forEach(function (m) {
  const nameHit = interesting.test(m.name) || interesting.test(m.path || '');
  let symHits = [];
  try {
    // enumerateExports is cheap; enumerateSymbols can be large, so exports first.
    m.enumerateExports().forEach(function (e) {
      if (wantedSyms.indexOf(e.name) >= 0) symHits.push({ name: e.name, address: e.address.toString() });
    });
  } catch (_) {}
  if (nameHit || symHits.length) {
    emit({ event: 'module', name: m.name, base: m.base.toString(), size: m.size,
      path: m.path, sslExports: symHits });
  }
});

// Also search symbols (not just exports) for keylog in likely-static modules,
// since a statically linked BoringSSL may not export these names.
['libapp.so', 'libflutter.so', 'libdevconn.so'].forEach(function (target) {
  const m = Process.findModuleByName(target);
  if (!m) return;
  let found = [];
  try {
    m.enumerateSymbols().forEach(function (s) {
      if (/keylog|SSL_read|SSL_write|SSL_do_handshake|client_random|master_key/i.test(s.name)) {
        found.push({ name: s.name, address: s.address.toString(), type: s.type });
      }
    });
  } catch (e) { emit({ event: 'symwalk-error', module: target, error: String(e) }); return; }
  emit({ event: 'static-symbols', module: target, matches: found.slice(0, 40), total: found.length });
});

emit({ event: 'done' });

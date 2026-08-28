'use strict';

/*
 * ARM recon — the first experiment to run on the arm64-v8a AVD.
 *
 * Purpose: prove the central hypothesis that on a native ARM environment the
 * Flutter/Magic modules become visible to Frida (they were invisible on the
 * x86 emulator because they ran ARMv7 behind the houdini native-bridge), and
 * report whether the BoringSSL TLS I/O symbols are resolvable by name. The
 * outcome decides the capture method:
 *   - SSL_read/SSL_write exported in libflutter.so  -> direct hook (dump script)
 *   - stripped/internal                             -> pattern scan or mitm route
 *
 * This script only reports; it hooks nothing and dumps no plaintext.
 */

function now() { return new Date().toISOString(); }
function emit(o) { console.log('[RECON] ' + JSON.stringify(Object.assign({ ts: now() }, o))); }

var WANT = ['libflutter.so', 'libapp.so', 'libdevconn.so', 'libssl.so',
            'libcrypto.so', 'libjavacrypto.so', 'libc.so'];
var TLS_SYMS = ['SSL_read', 'SSL_write', 'SSL_get_servername', 'SSL_get_fd',
                'SSL_CTX_set_keylog_callback', 'SSL_new'];

emit({ event: 'process', arch: Process.arch, pointerSize: Process.pointerSize,
       pageSize: Process.pageSize, id: Process.id,
       note: Process.pointerSize === 4 ? '32-bit process (armeabi-v7a app libs)'
                                       : '64-bit process' });

var modules = Process.enumerateModules();
emit({ event: 'module-count', count: modules.length });

// Report the modules we care about, present or absent.
WANT.forEach(function (name) {
  var m = null;
  for (var i = 0; i < modules.length; i++) {
    if (modules[i].name === name) { m = modules[i]; break; }
  }
  if (m) {
    emit({ event: 'module', name: name, present: true, base: m.base.toString(),
           size: m.size, path: m.path });
  } else {
    emit({ event: 'module', name: name, present: false });
  }
});

// Any module whose name hints at the vendor stack, so we do not miss a rename.
modules.forEach(function (m) {
  var n = m.name.toLowerCase();
  if (n.indexOf('flutter') >= 0 || n.indexOf('devconn') >= 0 ||
      n.indexOf('magic') >= 0 || n.indexOf('orbweb') >= 0 || n.indexOf('5gen') >= 0) {
    emit({ event: 'vendor-module', name: m.name, base: m.base.toString(), size: m.size, path: m.path });
  }
});

// Symbol resolution: global scope and per interesting module.
TLS_SYMS.forEach(function (sym) {
  var g = null;
  try { g = Module.findGlobalExportByName ? Module.findGlobalExportByName(sym) : null; } catch (e) {}
  emit({ event: 'symbol-global', symbol: sym, address: g ? g.toString() : null });
});

['libflutter.so', 'libssl.so', 'libapp.so'].forEach(function (mod) {
  TLS_SYMS.forEach(function (sym) {
    var a = null;
    try { a = Module.findExportByName(mod, sym); } catch (e) {}
    if (a) emit({ event: 'symbol-in-module', module: mod, symbol: sym, address: a.toString() });
  });
});

// Also enumerate libflutter.so's exports that look TLS-related, in case of renames.
try {
  var fl = Process.findModuleByName('libflutter.so');
  if (fl) {
    var hits = 0;
    fl.enumerateExports().forEach(function (e) {
      if (/ssl|tls|boring|x509|crypto/i.test(e.name)) {
        if (hits < 40) emit({ event: 'flutter-export', name: e.name, address: e.address.toString() });
        hits++;
      }
    });
    emit({ event: 'flutter-export-summary', tlsLikeExports: hits });
  }
} catch (e) { emit({ event: 'error', where: 'flutter-exports', error: String(e) }); }

emit({ event: 'ready' });

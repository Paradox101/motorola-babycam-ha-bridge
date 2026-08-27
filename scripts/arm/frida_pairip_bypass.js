'use strict';

/*
 * PairIP license-check bypass (DEX/Java variant, no libpairipcore).
 *
 * The reFlutter-patched, re-signed APK fails PairIP's Google Play license
 * verification, so LicenseClient.handleError() launches LicenseActivity's
 * "Check that Google Play is enabled" dialog and schedules System.exit(0).
 * PairIP here is pure Java on the ART runtime, which is x86 and therefore
 * hookable by x86 Frida (unlike the ARM-native Flutter BoringSSL).
 *
 * Strategy: no-op the whole license check and neutralise every shutdown/error
 * path, so the resigned app runs and we can reach login for the TLS capture.
 */

function log(m) { console.log('[PAIRIP] ' + m); }

Java.perform(function () {
  function neutralize(className, edits) {
    try {
      var C = Java.use(className);
      edits(C);
      log('patched ' + className);
    } catch (e) {
      log('skip ' + className + ' (' + e + ')');
    }
  }

  // 1) Skip the license check entirely.
  neutralize('com.pairip.licensecheck.LicenseClient', function (C) {
    C.checkLicense.implementation = function (ctx) { log('checkLicense() suppressed'); };
    // Belt-and-suspenders: even if invoked, error/exit paths do nothing.
    ['handleError', 'startErrorDialogActivity', 'startPaywallActivity',
     'scheduleAppShutdown', 'retryOrThrow'].forEach(function (m) {
      if (C[m]) {
        C[m].overloads.forEach(function (ov) {
          ov.implementation = function () { log(m + '() suppressed'); };
        });
      }
    });
  });

  // 2) The exit Runnable that calls System.exit(0).
  neutralize('com.pairip.licensecheck.LicenseClient$1', function (C) {
    C.run.implementation = function () { log('exitAction suppressed'); };
  });

  // 3) The error/paywall activity itself: never close the app.
  neutralize('com.pairip.licensecheck.LicenseActivity', function (C) {
    if (C.onStart) C.onStart.implementation = function () { log('LicenseActivity.onStart suppressed'); this.finish(); };
    if (C.exitApp) C.exitApp.implementation = function () { log('LicenseActivity.exitApp suppressed'); };
    if (C.closeAllTasks) C.closeAllTasks.implementation = function () { log('closeAllTasks suppressed'); };
  });

  // 4) Last resort: swallow System.exit(0) coming from the pairip package.
  var Sys = Java.use('java.lang.System');
  Sys.exit.implementation = function (code) {
    var stack = Java.use('android.util.Log').getStackTraceString(Java.use('java.lang.Exception').$new());
    if (stack.indexOf('pairip') >= 0) { log('blocked System.exit(' + code + ') from pairip'); return; }
    log('allowing System.exit(' + code + ')');
    return this.exit(code);
  };

  log('bypass installed');
});

'use strict';

/*
 * Passive Motorola Nursery / VM65 runtime observer.
 * It does not alter return values, issue requests, or invoke camera methods.
 * Account/control passwords and tokens are never printed. RTSP credentials are
 * printed because discovering those is the explicit purpose of this script.
 */

const state = {
  sid: null,
  serverId: null,
  cameraLanIp: null,
  connectionType: null,
  connectionTypeCode: null,
  remoteRtspPort: null,
  localMappedPort: null,
  rtspPoint: null,
  rtspUrl: null,
  rtspUser: null,
  rtspPassword: null,
  aliveUrls: null,
  channelId: null
};

function emit(event, data) {
  const line = Object.assign({ ts: new Date().toISOString(), event: event }, data || {});
  console.log('[VM65] ' + JSON.stringify(line));
}

function safeString(value) {
  try { return value === null || value === undefined ? null : String(value); }
  catch (_) { return '<unprintable>'; }
}

function listString(list) {
  if (list === null || list === undefined) return null;
  try { return String(list.toString()); } catch (_) { return '<list>'; }
}

function connectionType(code) {
  const n = Number(code);
  if (n === 1) return 'LAN';
  if (n === 2) return 'P2P/TCP';
  if (n === 4) return 'P2P/UDP';
  if (n === 6 || n === 7) return 'P2P';
  if (n === 8) return 'relay';
  if (n === 9) return 'relay/LAN';
  return n > 0 ? 'unknown(' + n + ')' : 'not-connected(' + n + ')';
}

function updateSummary() {
  emit('SUMMARY', {
    rtspUrl: state.rtspUrl,
    connectionType: state.connectionType,
    connectionTypeCode: state.connectionTypeCode,
    localMappedPort: state.localMappedPort,
    cameraRtspPort: state.remoteRtspPort,
    rtspPoint: state.rtspPoint,
    cameraSid: state.sid,
    serverId: state.serverId,
    cameraLanIp: state.cameraLanIp,
    channelId: state.channelId,
    aliveUrls: state.aliveUrls
  });
}

function readP2PManager(manager) {
  const out = {};
  if (!manager) return out;
  try { out.sid = safeString(manager.strSID.value); } catch (_) {}
  try { out.serverId = safeString(manager.clientP2PServerID.value); } catch (_) {}
  try { out.peerIp = safeString(manager.PeerIP.value); } catch (_) {}
  try { out.peerRealType = Number(manager.peerRealType.value); } catch (_) {}
  try { out.requestedP2PType = Number(manager.getCurrentP2PType()); } catch (_) {}
  try { out.portMap = safeString(manager.mPortMapList.value.toString()); } catch (_) {}
  if (out.sid) state.sid = out.sid;
  if (out.serverId) state.serverId = out.serverId;
  if (out.peerIp) state.cameraLanIp = out.peerIp;
  if (out.peerRealType !== undefined) {
    state.connectionTypeCode = out.peerRealType;
    state.connectionType = connectionType(out.peerRealType);
  }
  return out;
}

function inspectCameraInfo(info) {
  const out = {};
  if (!info) return out;
  const stringFields = ['sid', 'deviceID', 'name', 'rtsp_user', 'rtsp_password',
    'RTSP_POINT', 'local_ip'];
  const intFields = ['p2pType', 'RTSP_PORT', 'HTTP_PORT', 'CMD_PORT', 'errorCode'];
  stringFields.forEach(function (name) {
    try { out[name] = safeString(info[name].value); } catch (_) {}
  });
  intFields.forEach(function (name) {
    try { out[name] = Number(info[name].value); } catch (_) {}
  });
  try { out.remotePorts = safeString(info.remotePorts.value.toString()); } catch (_) {}
  /* Deliberately do not read CameraInfo.account or CameraInfo.password. */
  if (out.sid) state.sid = out.sid;
  if (out.local_ip) state.cameraLanIp = out.local_ip;
  if (out.RTSP_PORT > 0) state.remoteRtspPort = out.RTSP_PORT;
  if (out.RTSP_POINT) state.rtspPoint = out.RTSP_POINT;
  if (out.rtsp_user) state.rtspUser = out.rtsp_user;
  if (out.rtsp_password) state.rtspPassword = out.rtsp_password;
  if (out.p2pType !== undefined) {
    state.connectionTypeCode = out.p2pType;
    state.connectionType = connectionType(out.p2pType);
  }
  return out;
}

function hookMethod(klass, methodName, onEnter, onLeave) {
  if (!klass[methodName]) return;
  klass[methodName].overloads.forEach(function (ov) {
    ov.implementation = function () {
      const args = Array.prototype.slice.call(arguments);
      let context = null;
      try { context = onEnter ? onEnter.call(this, ov, args) : null; }
      catch (e) { emit('hook-error', { method: methodName, stage: 'enter', error: String(e) }); }
      const result = ov.apply(this, args);
      try { if (onLeave) onLeave.call(this, ov, args, result, context); }
      catch (e) { emit('hook-error', { method: methodName, stage: 'leave', error: String(e) }); }
      return result;
    };
  });
}

function hookJava() {
  Java.perform(function () {
    emit('loaded', { process: Process.id, arch: Process.arch });

    try {
      const Plugin = Java.use('com.example.orbweb.OrbwebPlugin');

      hookMethod(Plugin, 'setupCameraInfo', function (_ov, args) {
        return {
          name: safeString(args[0]), sid: safeString(args[1]),
          /* args[2] is the camera/control password: never log it. */
          rtspUser: safeString(args[3]), rtspPassword: safeString(args[4])
        };
      }, function (_ov, _args, result, context) {
        state.sid = context.sid;
        state.rtspUser = context.rtspUser;
        state.rtspPassword = context.rtspPassword;
        const camera = inspectCameraInfo(result);
        emit('OrbwebPlugin.setupCameraInfo', {
          name: context.name, sid: context.sid, accountPassword: '<redacted>',
          rtspUser: context.rtspUser, rtspPassword: context.rtspPassword,
          cameraInfo: camera
        });
      });

      hookMethod(Plugin, 'getPath', function () { return null; }, function (_ov, args, result) {
        const url = safeString(result);
        state.rtspUrl = url;
        try { inspectCameraInfo(args[0].getCameraInfo()); } catch (_) {}
        emit('RTSP_URL', { url: url });
        updateSummary();
      });

      hookMethod(Plugin, 'getPort', function () { return null; }, function (_ov, args, result) {
        state.localMappedPort = Number(result);
        try { inspectCameraInfo(args[0].getCameraInfo()); } catch (_) {}
        emit('OrbwebPlugin.getPort', { localMappedPort: state.localMappedPort });
      });

      hookMethod(Plugin, 'startM2M', function (_ov, args) {
        emit('OrbwebPlugin.startM2M', {
          name: safeString(args[0]), sid: safeString(args[1]),
          accountPassword: '<redacted>', rtspUser: safeString(args[3]),
          rtspPassword: safeString(args[4])
        });
        state.sid = safeString(args[1]);
        state.rtspUser = safeString(args[3]);
        state.rtspPassword = safeString(args[4]);
        return null;
      });
    } catch (e) { emit('class-unavailable', { className: 'OrbwebPlugin', error: String(e) }); }

    try {
      const Manager = Java.use('com.orbweb.liborbwebiot.OrbwebP2PManager');
      hookMethod(Manager, 'CreateP2PManagerFromID', function (ov, args) {
        const n = args.length;
        const data = {
          overload: ov.argumentTypes.map(t => t.className).join(','),
          sid: safeString(args[1]), requestedP2PType: Number(args[2]),
          rdzServer: safeString(args[3]), remotePorts: listString(args[4])
        };
        /* The final two String arguments on 10-arg overloads are credentials. */
        if (n >= 10) data.trailingCredentials = '<redacted>';
        state.sid = data.sid;
        emit('CreateP2PManagerFromID', data);
        return null;
      });

      hookMethod(Manager, 'ConnectWithAuth', function (_ov, args) {
        emit('ConnectWithAuth', Object.assign(readP2PManager(args[0]), {
          credentials: '<redacted>'
        }));
        return null;
      });

      hookMethod(Manager, 'CGI_GetRTSPInfo', function (_ov, args) {
        emit('CGI_GetRTSPInfo.call', {
          localCommandPort: Number(args[0]),
          arg1: '<redacted-or-server-id>', arg2: '<redacted>'
        });
        return null;
      });

      hookMethod(Manager, 'MapPort', function (_ov, args) {
        emit('MapPort.call', Object.assign(readP2PManager(args[0]), {
          remoteCameraPort: Number(args[1])
        }));
        if (Number(args[1]) === state.remoteRtspPort) state.remoteRtspPort = Number(args[1]);
        return { manager: args[0], remotePort: Number(args[1]) };
      }, function (_ov, _args, _result, context) {
        /* Mapping is async; P2PManager.getLocalPort/StartPortMapping records result. */
        emit('MapPort.return', Object.assign(readP2PManager(context.manager), {
          remoteCameraPort: context.remotePort
        }));
      });
    } catch (e) { emit('class-unavailable', { className: 'OrbwebP2PManager', error: String(e) }); }

    try {
      const P2P = Java.use('com.orbweb.m2m.P2PManager');
      hookMethod(P2P, 'StartConnectHost', function () {
        emit('P2PManager.StartConnectHost', readP2PManager(this)); return null;
      });
      hookMethod(P2P, 'StartPortMapping', function (_ov, args) {
        emit('P2PManager.StartPortMapping.call', Object.assign(readP2PManager(this), {
          serverIdArgument: safeString(args[0]), remoteCameraPort: Number(args[1]),
          firstCandidateLocalPort: Number(args[2])
        }));
        return null;
      }, function (_ov, args, result) {
        const remote = Number(args[1]);
        const local = Number(result);
        if (remote === state.remoteRtspPort || remote === 6667) {
          state.remoteRtspPort = remote;
          state.localMappedPort = local;
        }
        emit('PORT_MAPPING', { remoteCameraPort: remote, localMappedPort: local });
        updateSummary();
      });
      hookMethod(P2P, 'getLocalPort', function (_ov, args) {
        return { remote: Number(args[0]) };
      }, function (_ov, _args, result, context) {
        const local = Number(result);
        if (context.remote === state.remoteRtspPort || context.remote === 6667) {
          state.remoteRtspPort = context.remote;
          state.localMappedPort = local;
        }
        emit('P2PManager.getLocalPort', {
          remoteCameraPort: context.remote, localMappedPort: local
        });
      });
    } catch (e) { emit('class-unavailable', { className: 'P2PManager', error: String(e) }); }

    try {
      const Dev = Java.use('com.orbweb.libm2m.manager.M2MDeviceManager');
      ['getSID', 'getServerID', 'getLocalIP', 'getP2PType', 'getRTSPPoint'].forEach(function (name) {
        hookMethod(Dev, name, function () { return null; }, function (_ov, _args, result) {
          const value = safeString(result);
          if (name === 'getSID') state.sid = value;
          if (name === 'getServerID') state.serverId = value;
          if (name === 'getLocalIP') state.cameraLanIp = value;
          if (name === 'getP2PType') {
            state.connectionTypeCode = Number(result);
            state.connectionType = connectionType(result);
          }
          if (name === 'getRTSPPoint') state.rtspPoint = value;
          emit('DeviceApi.' + name, { value: value, type: name === 'getP2PType' ? connectionType(result) : undefined });
        });
      });
      hookMethod(Dev, 'getLocalPort', function (_ov, args) {
        return { remote: Number(args[0]), connectionSelector: args.length > 1 ? Number(args[1]) : null };
      }, function (_ov, _args, result, context) {
        const local = Number(result);
        if (context.remote === state.remoteRtspPort || context.remote === 6667) {
          state.remoteRtspPort = context.remote;
          state.localMappedPort = local;
        }
        emit('DeviceApi.getLocalPort', {
          remoteCameraPort: context.remote, localMappedPort: local,
          connectionSelector: context.connectionSelector
        });
      });
      hookMethod(Dev, 'getCameraInfo', function () { return null; }, function (_ov, _args, result) {
        emit('DeviceApi.getCameraInfo', { cameraInfo: inspectCameraInfo(result) });
      });
    } catch (e) { emit('class-unavailable', { className: 'M2MDeviceManager', error: String(e) }); }

    try {
      const Task = Java.use('com.orbweb.m2m.ORBConnectTask');
      hookMethod(Task, 'AddNewPort', function (_ov, args) {
        return { remote: Number(args[0]) };
      }, function (_ov, _args, result, context) {
        emit('ORBConnectTask.AddNewPort', {
          sid: safeString(this.getSID()), serverId: safeString(this.serverId.value),
          connectionTypeCode: Number(this.m2mConnectionType.value),
          connectionType: connectionType(this.m2mConnectionType.value),
          peerAddress: safeString(this.peerAddress.value),
          remoteCameraPort: context.remote, localMappedPort: Number(result)
        });
        state.sid = safeString(this.getSID());
        state.serverId = safeString(this.serverId.value);
        state.cameraLanIp = safeString(this.peerAddress.value);
        state.connectionTypeCode = Number(this.m2mConnectionType.value);
        state.connectionType = connectionType(this.m2mConnectionType.value);
        if (context.remote === 6667) {
          state.remoteRtspPort = 6667;
          state.localMappedPort = Number(result);
        }
      });
    } catch (e) { emit('class-unavailable', { className: 'ORBConnectTask', error: String(e) }); }

    try {
      const Tunnel = Java.use('com.orbweb.m2m.TunnelAPIs');
      ['startConnClient', 'startConnClient2', 'startClientLan',
       'GetClientTunnelConnType', 'GetClientTunnelPeerAddress'].forEach(function (name) {
        hookMethod(Tunnel, name, function (ov, args) {
          emit('TunnelAPIs.' + name + '.call', {
            signature: ov.argumentTypes.map(t => t.className).join(','),
            args: args.map(function (v, i) {
              /* SID/server identifiers are okay; no account password is passed here. */
              return i < 5 ? safeString(v) : '<omitted>';
            })
          });
          return null;
        }, function (_ov, _args, result) {
          emit('TunnelAPIs.' + name + '.return', { result: safeString(result) });
        });
      });
      hookMethod(Tunnel, 'addClientPortMapping', function (_ov, args) {
        return { serverId: safeString(args[0]), local: Number(args[1]), remote: Number(args[2]) };
      }, function (_ov, _args, result, context) {
        emit('TunnelAPIs.addClientPortMapping', {
          serverId: context.serverId, localMappedPort: context.local,
          remoteCameraPort: context.remote, resultCode: Number(result)
        });
      });
    } catch (e) { emit('class-unavailable', { className: 'TunnelAPIs', error: String(e) }); }

    /* Capture the actual URL at the Flutter player boundary and IJK boundary. */
    try {
      const Fijk = Java.use('com.befovy.fijkplayer.FijkPlayer');
      hookMethod(Fijk, 'onMethodCall', function (_ov, args) {
        try {
          if (safeString(args[0].method.value) === 'setDataSource') {
            const url = safeString(args[0].argument('url'));
            if (url && /^rtsp:\/\//i.test(url)) {
              state.rtspUrl = url;
              emit('FijkPlayer.setDataSource', { url: url });
              updateSummary();
            }
          }
        } catch (_) {}
        return null;
      });
    } catch (e) { emit('class-unavailable', { className: 'FijkPlayer', error: String(e) }); }

    try {
      const Ijk = Java.use('tv.danmaku.ijk.media.player.IjkMediaPlayer');
      hookMethod(Ijk, 'setDataSource', function (_ov, args) {
        const url = safeString(args[0]);
        if (url && /^rtsp:\/\//i.test(url)) {
          state.rtspUrl = url;
          emit('IjkMediaPlayer.setDataSource', { url: url });
          updateSummary();
        }
        return null;
      });
    } catch (e) { emit('class-unavailable', { className: 'IjkMediaPlayer', error: String(e) }); }

    /* Old SDK path: raw CGI response and parsed AliveUrls. */
    try {
      const CgiCallback = Java.use('com.orbweb.liborbwebiot.OrbwebP2PManager$7');
      hookMethod(CgiCallback, 'onReqComplete', function (_ov, args) {
        const raw = safeString(args[1]);
        emit('CGI_GetRTSPInfo.raw', { success: Boolean(args[0]), json: raw });
        try {
          const parsed = JSON.parse(raw);
          state.aliveUrls = parsed.AliveUrl || parsed.AliveUrls || null;
          if (state.aliveUrls && state.aliveUrls.length) {
            state.channelId = state.aliveUrls[0].CHANNEL_ID;
            if (state.aliveUrls[0].URL) state.rtspUrl = state.aliveUrls[0].URL;
          }
          updateSummary();
        } catch (_) {}
        return null;
      });
    } catch (e) {
      emit('info', { message: 'Legacy CGI callback class not loaded/used; modern OrbwebPlugin path remains hooked.' });
    }

    installNativeHooks();
  });
}

function installNativeHooks() {
  const names = [
    'Java_com_orbweb_m2m_TunnelAPIs_addClientPortMapping',
    'Java_com_orbweb_m2m_TunnelAPIs_startConnClient',
    'Java_com_orbweb_m2m_TunnelAPIs_startConnClient2',
    'Java_com_orbweb_m2m_TunnelAPIs_startClientLan',
    'Java_com_orbweb_m2m_TunnelAPIs_GetClientTunnelConnType',
    'Java_com_orbweb_m2m_TunnelAPIs_GetClientTunnelPeerAddress'
  ];
  names.forEach(function (name) {
    const address = Module.findGlobalExportByName
      ? Module.findGlobalExportByName(name)
      : Module.findExportByName(null, name);
    if (!address) return;
    Interceptor.attach(address, {
      onEnter(args) {
        this.name = name;
        /* JNIEnv and jobject occupy args[0..1]. For mapping, args[3]/args[4]
         * are the integer local and remote ports. Java hooks log strings safely. */
        const data = { symbol: name, address: String(address) };
        if (name.indexOf('addClientPortMapping') !== -1) {
          data.localMappedPort = args[3].toInt32();
          data.remoteCameraPort = args[4].toInt32();
        }
        emit('native.enter', data);
      },
      onLeave(retval) { emit('native.leave', { symbol: this.name, retval: retval.toInt32() }); }
    });
  });
}

setImmediate(hookJava);

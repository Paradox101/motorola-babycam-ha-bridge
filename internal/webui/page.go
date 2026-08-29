package webui

// page is the camera console. Like the pairing page it is inlined and loads
// nothing from the network, so it renders in a house whose internet is down —
// which is exactly when someone wants to look at the nursery.
//
// Live video tries three transports in order, because no single one works
// everywhere:
//
//   - WebRTC is the fastest, under a second, but its media never passes through
//     Ingress: the browser talks to the host's UDP port directly. That is fine
//     on the local network and impossible through Nabu Casa or a reverse proxy.
//   - MSE runs over the WebSocket this add-on proxies, so it works wherever the
//     page itself loaded, at a second or so of latency.
//   - MJPEG needs nothing but an <img> and works when everything else is
//     blocked, at the cost of bandwidth and no audio.
//
// The badge on each card says which one is carrying the picture, because "it is
// laggy" and "it is not connecting" have different answers depending on that.
const page = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Motorola Nursery Bridge</title>
<style>
  :root {
    color-scheme: light dark;
    --bg: #f4f5f7; --card: #ffffff; --ink: #1f2124; --muted: #5f6368;
    --line: #dcdfe3; --accent: #03a9f4; --accent-ink: #ffffff;
    --ok: #1e8e3e; --bad: #c5221f; --warn: #b06000;
    --bad-bg: #fdecea; --bad-ink: #8c1d18;
  }
  @media (prefers-color-scheme: dark) {
    :root {
      --bg: #101316; --card: #1b1e22; --ink: #e8eaed; --muted: #9aa0a6;
      --line: #383c40; --accent: #4fc3f7; --accent-ink: #06202c;
      --ok: #81c995; --bad: #f28b82; --warn: #fdd663;
      --bad-bg: #3b1f1d; --bad-ink: #f2b8b5;
    }
  }
  * { box-sizing: border-box; }
  body {
    margin: 0; padding: 18px 16px 44px; background: var(--bg); color: var(--ink);
    font: 15px/1.5 system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
  }
  main { max-width: 74rem; margin: 0 auto; }
  header { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 14px; }
  h1 { font-size: 1.25rem; margin: 0; flex: 1 1 auto; }
  .chip {
    font-size: .76rem; padding: 3px 10px; border-radius: 999px;
    border: 1px solid var(--line); color: var(--muted); white-space: nowrap;
  }
  .chip.ok { color: var(--ok); border-color: currentColor; }
  .chip.bad { color: var(--bad); border-color: currentColor; }
  .grid { display: grid; gap: 16px; grid-template-columns: repeat(auto-fill, minmax(21rem, 1fr)); }
  .grid.focus { grid-template-columns: 1fr; max-width: 56rem; margin: 0 auto; }
  .grid.focus .cam.dimmed { display: none; }
  .cam { background: var(--card); border: 1px solid var(--line); border-radius: 14px; overflow: hidden; }
  .frame { position: relative; aspect-ratio: 16 / 9; background: #000; cursor: zoom-in; }
  .grid.focus .frame { cursor: zoom-out; }
  /* Only the picture fills the frame. This used to be .frame > *, which also
     stretched the badge — a pill with a 999px radius — across the whole card. */
  .frame video, .frame img { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: contain; }
  .frame video { z-index: 2; background: transparent; }
  .frame video.hidden, .frame img.live.hidden { display: none; }
  .frame img.still { z-index: 1; filter: saturate(.9); }
  .frame img.live { z-index: 2; }
  .badge {
    position: absolute; z-index: 4; left: 10px; top: 10px;
    width: auto; height: auto; font-size: .7rem;
    letter-spacing: .04em; text-transform: uppercase; padding: 3px 9px;
    border-radius: 999px; background: rgba(0,0,0,.62); color: #fff;
    pointer-events: none;
  }
  .why {
    position: absolute; z-index: 4; left: 10px; right: 10px; bottom: 10px;
    width: auto; height: auto; font-size: .72rem; line-height: 1.35;
    padding: 6px 9px; border-radius: 8px; background: rgba(0,0,0,.72);
    color: #fff; pointer-events: none;
  }
  .badge.live { background: rgba(30,142,62,.85); }
  .badge.warn { background: rgba(176,96,0,.85); }
  .body { padding: 13px 15px 15px; }
  .title { display: flex; align-items: baseline; justify-content: space-between; gap: 8px; }
  .title h2 { font-size: 1rem; margin: 0; }
  .temp { font-variant-numeric: tabular-nums; color: var(--muted); font-size: .9rem; }
  .meta { color: var(--muted); font-size: .82rem; margin: 3px 0 0; }
  .stats { display: flex; gap: 14px; flex-wrap: wrap; margin: 9px 0 0; font-size: .82rem; color: var(--muted); }
  .stats b { color: var(--ink); font-weight: 600; }
  .dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; margin-right: 6px; }
  .dot.ok { background: var(--ok); } .dot.bad { background: var(--bad); }
  .actions { display: flex; gap: 7px; flex-wrap: wrap; margin-top: 12px; }
  button {
    font: inherit; font-size: .84rem; padding: 6px 12px; border-radius: 8px;
    border: 1px solid var(--line); background: var(--bg); color: var(--ink); cursor: pointer;
  }
  button:hover:not(:disabled) { border-color: var(--accent); }
  button.on { background: var(--accent); color: var(--accent-ink); border-color: transparent; }
  button:disabled { opacity: .5; cursor: default; }
  .note { border-radius: 10px; padding: 11px 14px; margin-bottom: 14px; background: var(--bad-bg); color: var(--bad-ink); }
  .diag { background: var(--card); border: 1px solid var(--line); border-radius: 14px; padding: 14px 16px; margin-top: 16px; }
  .diag dl { display: grid; grid-template-columns: repeat(auto-fill, minmax(11rem, 1fr)); gap: 10px 16px; margin: 0; }
  .diag dt { color: var(--muted); font-size: .78rem; }
  .diag dd { margin: 2px 0 0; font-variant-numeric: tabular-nums; }
  .empty { color: var(--muted); text-align: center; padding: 44px 0; }
  .diag .actions { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 12px; }
  .diag .hint { margin: 10px 0 0; color: var(--warn); font-size: 13px; line-height: 1.45; }
  [hidden] { display: none !important; }
</style>
</head>
<body>
<main>
  <header>
    <h1>Motorola Nursery Bridge</h1>
    <span class="chip" id="version"></span>
    <span class="chip" id="media" hidden></span>
    <span class="chip" id="mqtt" hidden></span>
    <button id="diag-toggle">Diagnostics</button>
  </header>
  <div id="message" class="note" hidden></div>
  <div class="grid" id="cameras"></div>
  <p class="empty" id="empty" hidden>No cameras yet.</p>
  <section class="diag" id="diag" hidden>
    <dl>
      <div><dt>Uptime</dt><dd id="d-uptime">—</dd></div>
      <div><dt>Bridge restarts</dt><dd id="d-restarts">—</dd></div>
      <div><dt>Active sessions</dt><dd id="d-sessions">—</dd></div>
      <div><dt>Cameras serving</dt><dd id="d-serving">—</dd></div>
      <div><dt>Media server</dt><dd id="d-media">—</dd></div>
      <div><dt>MQTT</dt><dd id="d-mqtt">—</dd></div>
      <div><dt>Stream host</dt><dd id="d-host">—</dd></div>
    </dl>
    <p class="hint" id="host-hint" hidden></p>
    <div class="actions">
      <button id="media-restart" hidden>Restart media server</button>
      <button id="creds-refresh" hidden>Refresh credentials</button>
    </div>
  </section>
</main>
<script>
(function () {
  var el = function (id) { return document.getElementById(id); };
  var cards = {};
  var focused = null;

  function say(text) {
    el("message").textContent = text || "";
    el("message").hidden = !text;
  }

  function chip(node, on, text) {
    node.hidden = false;
    node.textContent = text;
    node.className = "chip " + (on ? "ok" : "bad");
  }

  function duration(seconds) {
    if (!seconds && seconds !== 0) { return "—"; }
    var days = Math.floor(seconds / 86400);
    var hours = Math.floor((seconds % 86400) / 3600);
    var minutes = Math.floor((seconds % 3600) / 60);
    if (days) { return days + "d " + hours + "h"; }
    if (hours) { return hours + "h " + minutes + "m"; }
    return minutes + "m";
  }

  function wsURL(path) {
    var url = new URL(path, location.href);
    url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
    return url.href;
  }

  // ---- the player -------------------------------------------------------
  // Each camera owns one Player. It walks the transports in order and stops at
  // the first that actually produces a picture; "connected" is not enough,
  // because WebRTC negotiates happily and then never delivers a packet when
  // its UDP port is unreachable.

  function Player(parts) {
    this.parts = parts;
    this.mode = null;
    this.pc = null;
    this.ws = null;
    this.watchdog = null;
    this.stopped = true;
  }

  Player.prototype.badge = function (text, kind) {
    this.parts.badge.textContent = text;
    this.parts.badge.className = "badge" + (kind ? " " + kind : "");
  };

  // note records why a transport gave up. A card that simply says
  // "unavailable" is not something anyone can act on; the reason each of the
  // three was refused is what says whether to look at the network, the media
  // server or the camera.
  Player.prototype.note = function (mode, reason) {
    this.reasons = this.reasons || {};
    this.reasons[mode] = reason;
  };

  Player.prototype.showReasons = function () {
    var reasons = this.reasons || {};
    var lines = ["webrtc", "mse", "mjpeg"].filter(function (mode) {
      return reasons[mode];
    }).map(function (mode) {
      return mode + ": " + reasons[mode];
    });
    this.parts.why.textContent = lines.join(" · ");
    this.parts.why.hidden = !lines.length;
  };

  Player.prototype.clearReasons = function () {
    this.reasons = {};
    this.parts.why.hidden = true;
    this.parts.why.textContent = "";
  };

  Player.prototype.start = function () {
    if (!this.stopped) { return; }
    this.stopped = false;
    this.clearReasons();
    this.attempt("webrtc");
  };

  Player.prototype.stop = function () {
    this.stopped = true;
    this.teardown();
    this.mode = null;
    this.parts.video.classList.add("hidden");
    this.parts.live.classList.add("hidden");
    this.parts.why.hidden = true;
    this.badge("paused");
    this.parts.play.textContent = "Watch live";
    this.parts.play.classList.remove("on");
  };

  Player.prototype.teardown = function () {
    clearTimeout(this.watchdog);
    this.watchdog = null;
    if (this.pc) { try { this.pc.close(); } catch (e) {} this.pc = null; }
    if (this.ws) { try { this.ws.onclose = null; this.ws.close(); } catch (e) {} this.ws = null; }
    var video = this.parts.video;
    video.srcObject = null;
    if (video.src) { URL.revokeObjectURL(video.src); video.removeAttribute("src"); video.load(); }
    this.parts.live.removeAttribute("src");
  };

  // playing resolves the attempt: a frame arrived, so this transport works.
  Player.prototype.playing = function (mode) {
    if (this.stopped || this.mode === mode) { return; }
    clearTimeout(this.watchdog);
    this.watchdog = null;
    this.mode = mode;
    this.parts.why.hidden = true;
    this.badge(mode === "mjpeg" ? "mjpeg" : mode, "live");
    this.parts.play.textContent = "Stop";
    this.parts.play.classList.add("on");
    this.parts.audio.disabled = mode === "mjpeg";
  };

  // next falls through to the transport after the one that failed. The order
  // is fixed, so a failure never loops back to something already refused.
  Player.prototype.next = function (from, reason) {
    if (this.stopped || this.mode) { return; }
    this.note(from, reason || "no picture");
    this.teardown();
    if (from === "webrtc") { this.attempt("mse"); return; }
    if (from === "mse") { this.attempt("mjpeg"); return; }
    this.badge("unavailable", "warn");
    this.showReasons();
    this.parts.play.textContent = "Retry";
    this.parts.play.classList.remove("on");
    this.stopped = true;
  };

  Player.prototype.attempt = function (mode) {
    if (this.stopped) { return; }
    this.badge("connecting " + mode);
    var self = this;
    // Nothing is trusted to fail loudly: a transport that neither errors nor
    // delivers a frame is the common case, so every attempt is timed. MJPEG
    // gets longest — starting an H264 transcode from cold is not quick.
    // The relay tunnel and a keyframe come before any transport can show
    // anything, so none of these budgets is generous.
    var budget = mode === "webrtc" ? 9000 : (mode === "mse" ? 12000 : 20000);
    this.watchdog = setTimeout(function () {
      self.next(mode, "timed out after " + Math.round(budget / 1000) + "s");
    }, budget);
    if (mode === "webrtc") { this.webrtc(); }
    else if (mode === "mse") { this.mse(); }
    else { this.mjpeg(); }
  };

  Player.prototype.webrtc = function () {
    if (typeof RTCPeerConnection === "undefined") {
      this.next("webrtc", "not supported by this browser");
      return;
    }
    var self = this;
    var video = this.parts.video;
    var pc = new RTCPeerConnection({ iceServers: [], bundlePolicy: "max-bundle" });
    this.pc = pc;
    pc.addTransceiver("video", { direction: "recvonly" });
    pc.addTransceiver("audio", { direction: "recvonly" });
    pc.ontrack = function (event) {
      video.srcObject = event.streams[0];
      video.classList.remove("hidden");
      self.parts.live.classList.add("hidden");
    };
    pc.onconnectionstatechange = function () {
      if (pc.connectionState === "failed" || pc.connectionState === "closed") {
        // The usual cause: go2rtc advertised only its container address, so
        // nothing outside the container can reach the media port.
        self.next("webrtc", "peer connection " + pc.connectionState);
      }
    };
    pc.createOffer().then(function (offer) {
      return pc.setLocalDescription(offer).then(function () {
        return fetch("api/webrtc?src=" + encodeURIComponent(self.parts.camera.stream), {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ type: offer.type, sdp: offer.sdp })
        });
      });
    }).then(function (response) {
      if (!response.ok) { throw new Error("signalling returned " + response.status); }
      return response.json();
    }).then(function (answer) {
      if (self.pc !== pc) { return; }
      return pc.setRemoteDescription(new RTCSessionDescription(answer));
    }).catch(function (error) {
      self.next("webrtc", (error && error.message) || "negotiation failed");
    });
  };

  Player.prototype.mse = function () {
    if (typeof MediaSource === "undefined") {
      this.next("mse", "not supported by this browser");
      return;
    }
    var self = this;
    var video = this.parts.video;
    var candidates = [
      "avc1.640029", "avc1.64002A", "avc1.640033",
      "hvc1.1.6.L153.B0", "mp4a.40.2", "mp4a.40.5", "flac", "opus"
    ].filter(function (codec) {
      return MediaSource.isTypeSupported('video/mp4; codecs="' + codec + '"');
    });
    if (!candidates.length) { this.next("mse", "no supported codec"); return; }

    var ws;
    try {
      ws = new WebSocket(wsURL("api/ws?src=" + encodeURIComponent(this.parts.camera.stream)));
    } catch (e) { this.next("mse", "websocket blocked"); return; }
    ws.binaryType = "arraybuffer";
    this.ws = ws;

    var source = null, buffer = null, queue = [];
    var flush = function () {
      if (!buffer || buffer.updating || !queue.length) { return; }
      try { buffer.appendBuffer(queue.shift()); } catch (e) { self.next("mse", "buffer rejected"); }
    };

    ws.onopen = function () {
      ws.send(JSON.stringify({ type: "mse", value: candidates.join(",") }));
    };
    ws.onerror = function () { self.next("mse", "websocket error"); };
    ws.onclose = function (event) {
      if (!self.mode) { self.next("mse", "websocket closed" + (event && event.code ? " (" + event.code + ")" : "")); }
    };
    ws.onmessage = function (event) {
      if (self.ws !== ws) { return; }
      if (typeof event.data === "string") {
        var message;
        try { message = JSON.parse(event.data); } catch (e) { return; }
        // go2rtc reports a stream it cannot serve on this same socket.
        if (message.type === "error") {
          self.next("mse", String(message.value || "refused").slice(0, 90));
          return;
        }
        if (message.type !== "mse" || !message.value) { return; }
        source = new MediaSource();
        video.src = URL.createObjectURL(source);
        video.classList.remove("hidden");
        self.parts.live.classList.add("hidden");
        source.addEventListener("sourceopen", function () {
          try {
            buffer = source.addSourceBuffer(message.value);
          } catch (e) { self.next("mse", "codec refused: " + message.value); return; }
          buffer.mode = "segments";
          buffer.addEventListener("updateend", flush);
          flush();
        }, { once: true });
        return;
      }
      queue.push(new Uint8Array(event.data));
      // A tab left in the background accumulates buffer it will never show.
      if (queue.length > 60) { queue.splice(0, queue.length - 30); }
      flush();
    };
  };

  Player.prototype.mjpeg = function () {
    var self = this;
    var image = this.parts.live;
    image.onload = function () {
      image.classList.remove("hidden");
      self.playing("mjpeg");
    };
    image.onerror = function () {
      // Leave nothing behind: a broken <img> renders as a torn-page icon over
      // the still that was perfectly fine.
      image.classList.add("hidden");
      image.removeAttribute("src");
      self.next("mjpeg", "stream refused");
    };
    this.parts.video.classList.add("hidden");
    var stream = this.parts.camera.mjpeg_stream || this.parts.camera.stream;
    image.src = "api/stream.mjpeg?src=" + encodeURIComponent(stream) + "&t=" + Date.now();
  };

  // ---- cards ------------------------------------------------------------

  function card(camera) {
    var root = document.createElement("div");
    root.className = "cam";
    root.innerHTML =
      '<div class="frame">' +
        '<img class="still" alt="">' +
        '<img class="live hidden" alt="">' +
        '<video class="hidden" autoplay muted playsinline></video>' +
        '<span class="badge">paused</span>' +
        '<span class="why" hidden></span>' +
      '</div>' +
      '<div class="body">' +
        '<div class="title"><h2></h2><span class="temp"></span></div>' +
        '<p class="meta"></p>' +
        '<div class="stats"><span class="link"></span><span class="viewers"></span></div>' +
        '<div class="actions">' +
          '<button class="play">Watch live</button>' +
          '<button class="audio" disabled>Sound on</button>' +
          '<button class="full">Fullscreen</button>' +
          '<button class="save">Save still</button>' +
          '<button class="copy">Copy RTSP URL</button>' +
          '<button class="restart">Restart</button>' +
        '</div>' +
      '</div>';

    var parts = {
      root: root, camera: camera,
      frame: root.querySelector(".frame"),
      still: root.querySelector("img.still"),
      live: root.querySelector("img.live"),
      video: root.querySelector("video"),
      badge: root.querySelector(".badge"),
      why: root.querySelector(".why"),
      name: root.querySelector("h2"),
      temp: root.querySelector(".temp"),
      meta: root.querySelector(".meta"),
      link: root.querySelector(".link"),
      viewers: root.querySelector(".viewers"),
      play: root.querySelector(".play"),
      audio: root.querySelector(".audio"),
      full: root.querySelector(".full"),
      save: root.querySelector(".save"),
      copy: root.querySelector(".copy"),
      restart: root.querySelector(".restart")
    };
    parts.player = new Player(parts);

    // A frame actually rendered is the only proof a transport works.
    parts.video.addEventListener("loadeddata", function () {
      parts.player.playing(parts.player.pc ? "webrtc" : "mse");
    });
    parts.video.addEventListener("playing", function () {
      parts.player.playing(parts.player.pc ? "webrtc" : "mse");
    });

    parts.play.addEventListener("click", function () {
      if (parts.player.stopped) { parts.player.start(); } else { parts.player.stop(); }
    });
    parts.audio.addEventListener("click", function () {
      parts.video.muted = !parts.video.muted;
      parts.audio.textContent = parts.video.muted ? "Sound on" : "Sound off";
      parts.audio.classList.toggle("on", !parts.video.muted);
      if (!parts.video.muted) { parts.video.play().catch(function () {}); }
    });
    parts.full.addEventListener("click", function () {
      if (document.fullscreenElement) { document.exitFullscreen(); }
      else if (parts.frame.requestFullscreen) { parts.frame.requestFullscreen(); }
    });
    parts.save.addEventListener("click", function () { save(parts); });
    parts.copy.addEventListener("click", function () { copy(parts); });
    parts.restart.addEventListener("click", function () { restart(parts); });
    parts.frame.addEventListener("click", function () { focus(parts.camera.id); });
    return parts;
  }

  function still(parts) {
    // A still can legitimately be missing — the media server may still be
    // starting the stream — and a failed <img> renders as a torn-page icon
    // over the whole card. Clear it instead and let the next poll try again.
    parts.still.onerror = function () {
      parts.still.removeAttribute("src");
      parts.still.hidden = true;
    };
    parts.still.onload = function () { parts.still.hidden = false; };
    parts.still.src = "camera-still?src=" + encodeURIComponent(parts.camera.stream) + "&t=" + Date.now();
  }

  function save(parts) {
    var link = document.createElement("a");
    link.href = "camera-still?src=" + encodeURIComponent(parts.camera.stream) + "&t=" + Date.now();
    link.download = parts.camera.stream + "-" + new Date().toISOString().replace(/[:.]/g, "-") + ".jpg";
    document.body.appendChild(link);
    link.click();
    link.remove();
  }

  function copy(parts) {
    var url = parts.camera.stream_url || "";
    if (!url) { return; }
    var done = function () {
      parts.copy.textContent = "Copied";
      setTimeout(function () { parts.copy.textContent = "Copy RTSP URL"; }, 1500);
    };
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(url).then(done, function () { window.prompt("RTSP URL", url); });
    } else {
      window.prompt("RTSP URL", url);
    }
  }

  function restart(parts) {
    parts.restart.disabled = true;
    parts.player.stop();
    fetch("api/cameras/restart", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ id: parts.camera.id })
    }).then(function (response) {
      say(response.ok ? "" : "That camera could not be restarted.");
    }).catch(function () {
      say("The add-on did not answer.");
    }).then(function () {
      setTimeout(function () { parts.restart.disabled = false; refresh(); }, 1500);
    });
  }

  function focus(id) {
    focused = focused === id ? null : id;
    el("cameras").classList.toggle("focus", focused !== null);
    Object.keys(cards).forEach(function (key) {
      cards[key].root.classList.toggle("dimmed", focused !== null && key !== focused);
    });
  }

  function update(parts, camera) {
    parts.camera = camera;
    parts.name.textContent = camera.name;
    parts.meta.textContent = camera.model ? camera.model + " · " + camera.stream : camera.stream;
    parts.link.innerHTML = '<span class="dot ' + (camera.serving ? "ok" : "bad") + '"></span>' +
      (camera.serving ? "Connected" : "Reconnecting");
    parts.viewers.innerHTML = "<b>" + camera.active_sessions + "</b> watching";
    parts.temp.textContent = typeof camera.temperature_celsius === "number"
      ? camera.temperature_celsius.toFixed(1) + " °C" : "";
    if (!camera.serving && !parts.player.stopped) { parts.player.stop(); }
  }

  function refresh() {
    return fetch("api/cameras").then(function (response) {
      if (!response.ok) { throw new Error("unavailable"); }
      return response.json();
    }).then(function (data) {
      el("version").textContent = data.version || "";
      chip(el("media"), data.go2rtc_ready, data.go2rtc_ready ? "Media server up" : "Media server down");
      if (data.mqtt_enabled) {
        chip(el("mqtt"), data.mqtt_connected, data.mqtt_connected ? "MQTT connected" : "MQTT disconnected");
      }
      var list = data.cameras || [];
      el("empty").hidden = list.length > 0;

      var serving = 0;
      var seen = {};
      list.forEach(function (camera) {
        seen[camera.id] = true;
        if (camera.serving) { serving++; }
        if (!cards[camera.id]) {
          cards[camera.id] = card(camera);
          el("cameras").appendChild(cards[camera.id].root);
          still(cards[camera.id]);
          // Start live straight away: a camera page that shows a frozen image
          // until you press something is not a camera page.
          if (camera.serving) { cards[camera.id].player.start(); }
        }
        update(cards[camera.id], camera);
      });
      Object.keys(cards).forEach(function (id) {
        if (!seen[id]) {
          cards[id].player.stop();
          cards[id].root.remove();
          delete cards[id];
        }
      });

      el("d-uptime").textContent = duration(data.uptime_seconds);
      el("d-restarts").textContent = data.reconnects;
      el("d-sessions").textContent = list.reduce(function (total, camera) {
        return total + camera.active_sessions;
      }, 0);
      el("d-serving").textContent = serving + " of " + list.length;
      el("d-media").textContent = data.go2rtc_ready ? "up" : "down";
      el("d-mqtt").textContent = data.mqtt_enabled ? (data.mqtt_connected ? "connected" : "disconnected") : "off";
      el("d-host").textContent = data.stream_host || "not set";
      hostHint(data.stream_host);
      el("media-restart").hidden = !data.can_restart_media;
      el("creds-refresh").hidden = !data.can_refresh_credentials;

      say(data.go2rtc_ready ? "" : "The media server is not answering, so live video and stills are unavailable.");
    }).catch(function () {
      say("Could not reach the add-on.");
    });
  }

  // stream_host is the address this add-on hands out for RTSP and WebRTC media.
  // The browser reached this page over an address that demonstrably works, so
  // when the two differ that is worth saying: it is the reason live video falls
  // back to MSE, and the reason an RTSP URL copied from here resolves nowhere.
  function hostHint(streamHost) {
    var hint = el("host-hint");
    var here = location.hostname;
    if (!streamHost || !here || streamHost === here) { hint.hidden = true; return; }
    hint.textContent = "This add-on advertises " + streamHost + " for RTSP and WebRTC, but you reached "
      + "this page at " + here + ". If live video keeps falling back to MSE, or an RTSP URL from here "
      + "does not resolve, set stream_host to an address your players can reach.";
    hint.hidden = false;
  }

  // act runs one repair from the diagnostics panel. Both take a moment and both
  // are worth confirming in place, because neither changes anything visible on
  // the page by itself.
  function act(button, path, busyText, doneText) {
    var original = button.textContent;
    button.disabled = true;
    button.textContent = busyText;
    fetch(path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: "{}"
    }).then(function (response) {
      if (!response.ok) { throw new Error("failed"); }
      button.textContent = doneText;
      say("");
    }).catch(function () {
      button.textContent = original;
      say("That did not work. The add-on log has the reason.");
    }).then(function () {
      setTimeout(function () {
        button.textContent = original;
        button.disabled = false;
        refresh();
      }, 2500);
    });
  }

  el("media-restart").addEventListener("click", function () {
    act(this, "api/media/restart", "Restarting…", "Restarting");
  });
  el("creds-refresh").addEventListener("click", function () {
    act(this, "api/credentials/refresh", "Refreshing…", "Refresh started");
  });

  el("diag-toggle").addEventListener("click", function () {
    var panel = el("diag");
    panel.hidden = !panel.hidden;
    el("diag-toggle").classList.toggle("on", !panel.hidden);
  });

  // A hidden tab should not keep pulling video over someone's connection.
  document.addEventListener("visibilitychange", function () {
    Object.keys(cards).forEach(function (id) {
      if (document.hidden) { cards[id].player.stop(); }
      else if (cards[id].camera.serving) { cards[id].player.start(); }
    });
  });

  document.addEventListener("keydown", function (event) {
    if (event.key === "Escape" && focused) { focus(focused); }
  });

  refresh();
  setInterval(refresh, 5000);
  setInterval(function () {
    Object.keys(cards).forEach(function (id) {
      if (cards[id].player.stopped) { still(cards[id]); }
    });
  }, 30000);
})();
</script>
</body>
</html>
`

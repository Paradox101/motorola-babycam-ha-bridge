package webui

// page is the camera view. Like the pairing page it is inlined and loads
// nothing from the network, so it renders in a house with no internet — which
// is exactly when someone wants to check the nursery camera.
//
// Video is WebRTC, negotiated against go2rtc through the proxy. WebRTC is what
// gives a sub-second picture; the snapshot underneath is what shows while the
// connection is still being set up, and what remains if it cannot be.
const page = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Motorola Nursery Bridge</title>
<style>
  :root {
    color-scheme: light dark;
    --bg: #f5f5f5; --card: #ffffff; --ink: #212121; --muted: #5f6368;
    --line: #dadce0; --accent: #03a9f4; --accent-ink: #ffffff;
    --ok: #1e8e3e; --bad: #c5221f; --warn: #e37400;
    --bad-bg: #fdecea; --bad-ink: #8c1d18;
  }
  @media (prefers-color-scheme: dark) {
    :root {
      --bg: #111417; --card: #1c1f23; --ink: #e8eaed; --muted: #9aa0a6;
      --line: #3c4043; --accent: #03a9f4; --accent-ink: #06202c;
      --ok: #81c995; --bad: #f28b82; --warn: #fdd663;
      --bad-bg: #3b1f1d; --bad-ink: #f2b8b5;
    }
  }
  * { box-sizing: border-box; }
  body {
    margin: 0; padding: 20px 16px 40px; background: var(--bg); color: var(--ink);
    font: 15px/1.5 system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
  }
  main { max-width: 60rem; margin: 0 auto; }
  header { display: flex; align-items: baseline; gap: 12px; flex-wrap: wrap; margin-bottom: 16px; }
  h1 { font-size: 1.3rem; margin: 0; }
  .chip {
    font-size: .78rem; padding: 2px 9px; border-radius: 999px;
    border: 1px solid var(--line); color: var(--muted);
  }
  .chip.ok { color: var(--ok); border-color: currentColor; }
  .chip.bad { color: var(--bad); border-color: currentColor; }
  .grid { display: grid; gap: 16px; grid-template-columns: repeat(auto-fill, minmax(20rem, 1fr)); }
  .cam { background: var(--card); border: 1px solid var(--line); border-radius: 12px; overflow: hidden; }
  .frame { position: relative; aspect-ratio: 16 / 9; background: #000; }
  .frame video, .frame img { width: 100%; height: 100%; object-fit: contain; display: block; }
  .frame img { position: absolute; inset: 0; }
  .frame video { position: relative; z-index: 1; background: transparent; }
  .frame video.idle { display: none; }
  .badge {
    position: absolute; z-index: 2; left: 8px; top: 8px; font-size: .72rem;
    padding: 2px 8px; border-radius: 999px; background: rgba(0,0,0,.62); color: #fff;
  }
  .body { padding: 14px 16px 16px; }
  .title { display: flex; align-items: baseline; justify-content: space-between; gap: 8px; }
  .title h2 { font-size: 1.02rem; margin: 0; }
  .meta { color: var(--muted); font-size: .84rem; margin: 4px 0 0; }
  .stats { display: flex; gap: 14px; flex-wrap: wrap; margin: 10px 0 0; font-size: .84rem; }
  .stats span { color: var(--muted); }
  .stats b { color: var(--ink); font-weight: 600; }
  .dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; margin-right: 6px; }
  .dot.ok { background: var(--ok); } .dot.bad { background: var(--bad); }
  .actions { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 14px; }
  button {
    font: inherit; font-size: .86rem; padding: 7px 13px; border-radius: 8px;
    border: 1px solid var(--line); background: var(--bg); color: var(--ink); cursor: pointer;
  }
  button.primary { background: var(--accent); color: var(--accent-ink); border-color: transparent; font-weight: 600; }
  button:disabled { opacity: .55; cursor: default; }
  .url {
    margin-top: 12px; font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: .78rem; color: var(--muted); word-break: break-all;
  }
  .note { border-radius: 8px; padding: 12px 14px; margin-bottom: 16px; background: var(--bad-bg); color: var(--bad-ink); }
  .empty { color: var(--muted); text-align: center; padding: 40px 0; }
  [hidden] { display: none !important; }
</style>
</head>
<body>
<main>
  <header>
    <h1>Motorola Nursery Bridge</h1>
    <span class="chip" id="version"></span>
    <span class="chip" id="mqtt" hidden></span>
    <span class="chip" id="media" hidden></span>
  </header>
  <div id="message" class="note" hidden></div>
  <div class="grid" id="cameras"></div>
  <p class="empty" id="empty" hidden>No cameras yet.</p>
</main>
<script>
(function () {
  var el = function (id) { return document.getElementById(id); };
  var cards = {};

  function say(text) {
    el("message").textContent = text || "";
    el("message").hidden = !text;
  }

  function chip(node, on, text) {
    node.hidden = false;
    node.textContent = text;
    node.className = "chip " + (on ? "ok" : "bad");
  }

  function card(camera) {
    var root = document.createElement("div");
    root.className = "cam";
    root.innerHTML =
      '<div class="frame">' +
        '<img alt="">' +
        '<video class="idle" autoplay muted playsinline></video>' +
        '<span class="badge"></span>' +
      '</div>' +
      '<div class="body">' +
        '<div class="title"><h2></h2><span class="temp"></span></div>' +
        '<p class="meta"></p>' +
        '<div class="stats">' +
          '<span class="link"></span><span class="viewers"></span>' +
        '</div>' +
        '<div class="actions">' +
          '<button class="primary play">Watch live</button>' +
          '<button class="refresh">Refresh still</button>' +
          '<button class="copy">Copy RTSP URL</button>' +
          '<button class="restart">Restart bridge</button>' +
        '</div>' +
        '<div class="url"></div>' +
      '</div>';

    var parts = {
      root: root,
      img: root.querySelector("img"),
      video: root.querySelector("video"),
      badge: root.querySelector(".badge"),
      name: root.querySelector("h2"),
      temp: root.querySelector(".temp"),
      meta: root.querySelector(".meta"),
      link: root.querySelector(".link"),
      viewers: root.querySelector(".viewers"),
      play: root.querySelector(".play"),
      refresh: root.querySelector(".refresh"),
      copy: root.querySelector(".copy"),
      restart: root.querySelector(".restart"),
      url: root.querySelector(".url"),
      pc: null,
      camera: camera
    };

    parts.play.addEventListener("click", function () { toggle(parts); });
    parts.refresh.addEventListener("click", function () { still(parts); });
    parts.copy.addEventListener("click", function () { copy(parts); });
    parts.restart.addEventListener("click", function () { restart(parts); });
    return parts;
  }

  function still(parts) {
    parts.img.src = "camera-still?src=" + encodeURIComponent(parts.camera.stream) + "&t=" + Date.now();
  }

  function copy(parts) {
    var url = parts.camera.stream_url || "";
    if (!url) { return; }
    var done = function () {
      parts.copy.textContent = "Copied";
      setTimeout(function () { parts.copy.textContent = "Copy RTSP URL"; }, 1500);
    };
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(url).then(done, function () { parts.url.textContent = url; });
    } else {
      parts.url.textContent = url;
    }
  }

  function restart(parts) {
    parts.restart.disabled = true;
    fetch("api/cameras/restart", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ id: parts.camera.id })
    }).then(function (response) {
      if (!response.ok) { say("That camera could not be restarted."); }
      else { say(""); }
    }).catch(function () {
      say("The add-on did not answer.");
    }).then(function () {
      setTimeout(function () { parts.restart.disabled = false; refresh(); }, 1500);
    });
  }

  function stop(parts) {
    if (parts.pc) { parts.pc.close(); parts.pc = null; }
    parts.video.srcObject = null;
    parts.video.className = "idle";
    parts.badge.textContent = "";
    parts.play.textContent = "Watch live";
  }

  function toggle(parts) {
    if (parts.pc) { stop(parts); return; }
    parts.play.disabled = true;
    parts.badge.textContent = "connecting";
    var pc = new RTCPeerConnection({ iceServers: [] });
    parts.pc = pc;
    pc.addTransceiver("video", { direction: "recvonly" });
    pc.addTransceiver("audio", { direction: "recvonly" });
    pc.ontrack = function (event) {
      parts.video.srcObject = event.streams[0];
      parts.video.className = "";
      parts.badge.textContent = "live";
      parts.play.textContent = "Stop";
    };
    pc.onconnectionstatechange = function () {
      if (pc.connectionState === "failed" || pc.connectionState === "disconnected") {
        stop(parts);
        say("The live connection to " + parts.camera.name + " dropped.");
      }
    };
    pc.createOffer().then(function (offer) {
      return pc.setLocalDescription(offer).then(function () {
        return fetch("api/webrtc?src=" + encodeURIComponent(parts.camera.stream), {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ type: offer.type, sdp: offer.sdp })
        });
      });
    }).then(function (response) {
      if (!response.ok) { throw new Error("negotiation refused"); }
      return response.json();
    }).then(function (answer) {
      return pc.setRemoteDescription(new RTCSessionDescription(answer));
    }).catch(function () {
      stop(parts);
      say("Could not start live video for " + parts.camera.name + ".");
    }).then(function () {
      parts.play.disabled = false;
    });
  }

  function update(parts, camera) {
    parts.camera = camera;
    parts.name.textContent = camera.name;
    parts.meta.textContent = camera.model ? camera.model + " · " + camera.stream : camera.stream;
    parts.link.innerHTML = '<span class="dot ' + (camera.serving ? "ok" : "bad") + '"></span>' +
      (camera.serving ? "Connected" : "Reconnecting");
    parts.viewers.innerHTML = "<b>" + camera.active_sessions + "</b> <span>watching</span>";
    parts.temp.textContent = typeof camera.temperature_celsius === "number"
      ? camera.temperature_celsius.toFixed(1) + " °C" : "";
    parts.restart.disabled = false;
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

      var seen = {};
      list.forEach(function (camera) {
        seen[camera.id] = true;
        if (!cards[camera.id]) {
          cards[camera.id] = card(camera);
          el("cameras").appendChild(cards[camera.id].root);
          still(cards[camera.id]);
        }
        update(cards[camera.id], camera);
      });
      Object.keys(cards).forEach(function (id) {
        if (!seen[id]) {
          stop(cards[id]);
          cards[id].root.remove();
          delete cards[id];
        }
      });
      if (!data.go2rtc_ready) {
        say("The media server is not answering, so live video and stills are unavailable.");
      } else {
        say("");
      }
    }).catch(function () {
      say("Could not reach the add-on.");
    });
  }

  refresh();
  setInterval(refresh, 5000);
  // Keep the still image current on cards that are not playing live video.
  setInterval(function () {
    Object.keys(cards).forEach(function (id) {
      if (!cards[id].pc) { still(cards[id]); }
    });
  }, 30000);
})();
</script>
</body>
</html>
`

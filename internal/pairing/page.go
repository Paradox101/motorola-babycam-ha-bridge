package pairing

// page is the whole pairing UI. It is inlined rather than served from disk so
// the add-on image cannot end up with a binary and no interface, and it loads
// nothing from the network: a setup screen that needs the internet to render is
// one that cannot be used to fix a broken setup.
//
// It follows the Home Assistant frame's own colours through prefers-color-scheme
// so it does not glare in a dark dashboard.
const page = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Pair Motorola Nursery Bridge</title>
<style>
  :root {
    color-scheme: light dark;
    --bg: #f5f5f5; --card: #ffffff; --ink: #212121; --muted: #5f6368;
    --line: #dadce0; --accent: #03a9f4; --accent-ink: #ffffff;
    --bad-bg: #fdecea; --bad-ink: #8c1d18; --good-bg: #e6f4ea; --good-ink: #1e6b34;
  }
  @media (prefers-color-scheme: dark) {
    :root {
      --bg: #111417; --card: #1c1f23; --ink: #e8eaed; --muted: #9aa0a6;
      --line: #3c4043; --accent: #03a9f4; --accent-ink: #06202c;
      --bad-bg: #3b1f1d; --bad-ink: #f2b8b5; --good-bg: #1d3025; --good-ink: #a8d5b8;
    }
  }
  * { box-sizing: border-box; }
  body {
    margin: 0; padding: 24px 16px; background: var(--bg); color: var(--ink);
    font: 15px/1.55 system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
  }
  main { max-width: 34rem; margin: 0 auto; }
  .card {
    background: var(--card); border: 1px solid var(--line); border-radius: 12px;
    padding: 24px; margin-bottom: 16px;
  }
  h1 { font-size: 1.35rem; margin: 0 0 4px; }
  p.lede { color: var(--muted); margin: 0 0 20px; }
  label { display: block; font-weight: 600; margin-bottom: 6px; }
  .hint { color: var(--muted); font-size: .875rem; margin: 6px 0 0; }
  input {
    width: 100%; padding: 10px 12px; font: inherit; color: var(--ink);
    background: var(--bg); border: 1px solid var(--line); border-radius: 8px;
  }
  input:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
  #code { font-size: 1.5rem; letter-spacing: .35em; text-align: center; }
  button {
    margin-top: 16px; padding: 10px 18px; font: inherit; font-weight: 600;
    color: var(--accent-ink); background: var(--accent); border: 0;
    border-radius: 8px; cursor: pointer;
  }
  button:disabled { opacity: .55; cursor: progress; }
  button.link {
    background: none; color: var(--accent); padding: 0; margin-left: 12px;
    font-weight: 400; text-decoration: underline;
  }
  .row { display: flex; align-items: center; flex-wrap: wrap; }
  .note { border-radius: 8px; padding: 12px 14px; margin-top: 16px; }
  .note.bad { background: var(--bad-bg); color: var(--bad-ink); }
  .note.good { background: var(--good-bg); color: var(--good-ink); }
  ol { margin: 0; padding-left: 1.2rem; color: var(--muted); }
  li { margin-bottom: 4px; }
  [hidden] { display: none !important; }
</style>
</head>
<body>
<main>
  <div class="card">
    <h1>Pair your Motorola Nursery account</h1>
    <p class="lede">Motorola emails a one-time code. Nothing here is stored in the
      add-on configuration, and the add-on does not need a restart.</p>

    <section id="step-email">
      <label for="email">Account email</label>
      <input id="email" type="email" autocomplete="email" inputmode="email"
             placeholder="you@example.com" autofocus>
      <p class="hint">The address you use in the Motorola Nursery app.</p>
      <button id="send">Send code</button>
    </section>

    <section id="step-code" hidden>
      <label for="code">Code from the email</label>
      <input id="code" type="text" inputmode="numeric" autocomplete="one-time-code"
             placeholder="000000">
      <p class="hint" id="code-hint"></p>
      <div class="row">
        <button id="verify">Finish pairing</button>
        <button id="resend" class="link" type="button">Send a new code</button>
      </div>
    </section>

    <section id="step-done" hidden>
      <p class="note good"><strong>Paired.</strong> The add-on is starting your
        cameras now. This page becomes the stream view once they are up — give it
        a moment, then reload.</p>
    </section>

    <div id="message" class="note bad" hidden></div>
  </div>

  <div class="card">
    <ol>
      <li>Enter the email address of your Motorola Nursery account.</li>
      <li>Open the email from Motorola and copy the code.</li>
      <li>Paste it here. Codes expire, so request a new one if it is refused.</li>
    </ol>
  </div>
</main>
<script>
(function () {
  var el = function (id) { return document.getElementById(id); };
  var message = el("message");
  var pairedEmail = "";

  function say(text, good) {
    message.textContent = text;
    message.className = "note " + (good ? "good" : "bad");
    message.hidden = !text;
  }

  function show(step) {
    el("step-email").hidden = step !== "email";
    el("step-code").hidden = step !== "code";
    el("step-done").hidden = step !== "done";
  }

  function render(status) {
    if (status.email) { pairedEmail = status.email; }
    if (status.paired) { show("done"); return; }
    if (status.awaiting_code) {
      show("code");
      var minutes = Math.max(1, Math.round((status.code_expires_in_seconds || 0) / 60));
      el("code-hint").textContent = status.email
        ? "Sent to " + status.email + ". Valid for about " + minutes + " more minute" + (minutes === 1 ? "" : "s") + "."
        : "Enter the code from the email.";
      el("code").focus();
      return;
    }
    show("email");
    if (status.email) { el("email").value = status.email; }
  }

  function post(path, body, button, onOK) {
    button.disabled = true;
    say("");
    fetch(path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body)
    }).then(function (response) {
      return response.json().then(function (data) {
        return { ok: response.ok, data: data };
      });
    }).then(function (result) {
      if (!result.ok) { say(result.data.error || "Something went wrong."); return; }
      onOK(result.data);
    }).catch(function () {
      say("The add-on did not answer. Is it still running?");
    }).then(function () {
      button.disabled = false;
    });
  }

  el("send").addEventListener("click", function () {
    var email = el("email").value.trim();
    if (!email) { say("Enter the email address of your Motorola account."); return; }
    post("api/pairing/code", { email: email }, el("send"), function (status) {
      render(status);
      say("Code sent. It can take a minute to arrive.", true);
    });
  });

  el("resend").addEventListener("click", function () {
    var email = pairedEmail || el("email").value.trim();
    if (!email) { show("email"); say("Enter the email address of your Motorola account."); return; }
    post("api/pairing/code", { email: email }, el("resend"), function (status) {
      render(status);
      say("A new code is on its way.", true);
    });
  });

  el("verify").addEventListener("click", function () {
    var code = el("code").value.trim();
    if (!code) { say("Enter the code from the email."); return; }
    post("api/pairing/verify", { code: code }, el("verify"), function (status) {
      render(status);
      say("");
    });
  });

  el("code").addEventListener("keydown", function (event) {
    if (event.key === "Enter") { el("verify").click(); }
  });
  el("email").addEventListener("keydown", function (event) {
    if (event.key === "Enter") { el("send").click(); }
  });

  fetch("api/pairing/status").then(function (response) {
    return response.json();
  }).then(render).catch(function () {
    say("Could not reach the add-on.");
  });
})();
</script>
</body>
</html>
`

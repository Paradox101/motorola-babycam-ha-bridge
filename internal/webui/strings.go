package webui

import "github.com/local/motorola-vm65-bridge/internal/i18n"

// Every string the page shows lives here. The mechanism — %%token%% in the
// markup, the same catalog handed to the script as a dictionary — is described
// in internal/i18n/page.go.

// catalogs holds every language's strings. Every catalog must carry the same
// keys as the English one; renderPages refuses to build a page otherwise, and
// the test asserts it, so a forgotten translation cannot ship as a raw token
// on the page.
var catalogs = map[i18n.Language]i18n.Catalog{
	i18n.English: {
		"htmlLang": "en",
		"title":    "Motorola Nursery Homeassistant Bridge",

		"diagnostics":       "Diagnostics",
		"noCameras":         "No cameras yet.",
		"uptime":            "Uptime",
		"bridgeRestarts":    "Bridge restarts",
		"streamConnections": "Stream connections",
		"camerasServing":    "Cameras serving",
		"mediaServer":       "Media server",
		"mqtt":              "MQTT",
		"streamHost":        "Stream host",
		"restartMedia":      "Restart media server",
		"refreshCredsBtn":   "Refresh credentials",

		"watchLive":  "Watch live",
		"stop":       "Stop",
		"retry":      "Retry",
		"soundOn":    "Sound on",
		"soundOff":   "Sound off",
		"fullscreen": "Fullscreen",
		"saveStill":  "Save still",
		"copyRTSP":   "Copy RTSP URL",
		"copied":     "Copied",
		"restart":    "Restart",
		"rtspURL":    "RTSP URL",

		"paused":      "paused",
		"unavailable": "unavailable",
		"connecting":  "connecting {mode}",

		"connected":         "Connected",
		"reconnecting":      "Reconnecting",
		"sessionOne":        "stream connection",
		"sessionMany":       "stream connections",
		"mediaUp":           "Media server up",
		"mediaDown":         "Media server down",
		"mqttConnected":     "MQTT connected",
		"mqttDisconnected":  "MQTT disconnected",
		"valueUp":           "up",
		"valueDown":         "down",
		"valueConnected":    "connected",
		"valueDisconnected": "disconnected",
		"valueOff":          "off",
		"notSet":            "not set",
		"servingOf":         "{serving} of {total}",

		"mediaDownNote":  "The media server is not answering, so live video and stills are unavailable.",
		"unreachable":    "Could not reach the add-on.",
		"restartFailed":  "That camera could not be restarted.",
		"noAnswer":       "The add-on did not answer.",
		"actionFailed":   "That did not work. The add-on log has the reason.",
		"restarting":     "Restarting…",
		"restartStarted": "Restarting",
		"refreshing":     "Refreshing…",
		"refreshStarted": "Refresh started",

		"hostHint": "This add-on advertises {advertised} for RTSP and WebRTC, but you reached this " +
			"page at {here}. If live video keeps falling back to MSE, or an RTSP URL from here does " +
			"not resolve, set stream_host to an address your players can reach.",

		"unitDay":    "d",
		"unitHour":   "h",
		"unitMinute": "m",

		"noPicture":         "no picture",
		"timedOut":          "timed out after {seconds}s",
		"notSupported":      "not supported by this browser",
		"peerState":         "peer connection {state}",
		"negotiationFailed": "negotiation failed",
		"signalling":        "signalling returned {status}",
		"noCodec":           "no supported codec",
		"websocketBlocked":  "websocket blocked",
		"websocketError":    "websocket error",
		"websocketClosed":   "websocket closed",
		"bufferRejected":    "buffer rejected",
		"codecRefused":      "codec refused: {codec}",
		"refused":           "refused",
		"streamRefused":     "stream refused",
	},
	i18n.Dutch: {
		"htmlLang": "nl",
		"title":    "Motorola Nursery Homeassistant Bridge",

		"diagnostics":       "Diagnose",
		"noCameras":         "Nog geen camera's.",
		"uptime":            "Draaitijd",
		"bridgeRestarts":    "Herstarts van de bridge",
		"streamConnections": "Streamverbindingen",
		"camerasServing":    "Actieve camera's",
		"mediaServer":       "Mediaserver",
		"mqtt":              "MQTT",
		"streamHost":        "Streamhost",
		"restartMedia":      "Mediaserver herstarten",
		"refreshCredsBtn":   "Inloggegevens vernieuwen",

		"watchLive":  "Live bekijken",
		"stop":       "Stoppen",
		"retry":      "Opnieuw proberen",
		"soundOn":    "Geluid aan",
		"soundOff":   "Geluid uit",
		"fullscreen": "Volledig scherm",
		"saveStill":  "Foto opslaan",
		"copyRTSP":   "RTSP-URL kopiëren",
		"copied":     "Gekopieerd",
		"restart":    "Herstarten",
		"rtspURL":    "RTSP-URL",

		"paused":      "gepauzeerd",
		"unavailable": "geen beeld",
		"connecting":  "verbinden via {mode}",

		"connected":         "Verbonden",
		"reconnecting":      "Opnieuw verbinden",
		"sessionOne":        "streamverbinding",
		"sessionMany":       "streamverbindingen",
		"mediaUp":           "Mediaserver actief",
		"mediaDown":         "Mediaserver onbereikbaar",
		"mqttConnected":     "MQTT verbonden",
		"mqttDisconnected":  "MQTT niet verbonden",
		"valueUp":           "actief",
		"valueDown":         "onbereikbaar",
		"valueConnected":    "verbonden",
		"valueDisconnected": "niet verbonden",
		"valueOff":          "uit",
		"notSet":            "niet ingesteld",
		"servingOf":         "{serving} van {total}",

		"mediaDownNote":  "De mediaserver antwoordt niet, dus live beeld en foto's zijn niet beschikbaar.",
		"unreachable":    "De add-on is niet bereikbaar.",
		"restartFailed":  "Deze camera kon niet worden herstart.",
		"noAnswer":       "De add-on gaf geen antwoord.",
		"actionFailed":   "Dat is niet gelukt. De reden staat in het logboek van de add-on.",
		"restarting":     "Herstarten…",
		"restartStarted": "Herstart gestart",
		"refreshing":     "Vernieuwen…",
		"refreshStarted": "Vernieuwen gestart",

		"hostHint": "Deze add-on geeft {advertised} op voor RTSP en WebRTC, maar je opende deze " +
			"pagina op {here}. Valt live beeld steeds terug op MSE, of komt een RTSP-URL van hier " +
			"nergens uit, stel stream_host dan in op een adres dat je spelers kunnen bereiken.",

		"unitDay":    "d",
		"unitHour":   "u",
		"unitMinute": "m",

		"noPicture":         "geen beeld",
		"timedOut":          "geen beeld na {seconds}s",
		"notSupported":      "niet ondersteund door deze browser",
		"peerState":         "peerverbinding {state}",
		"negotiationFailed": "onderhandeling mislukt",
		"signalling":        "signalering gaf {status}",
		"noCodec":           "geen ondersteunde codec",
		"websocketBlocked":  "websocket geblokkeerd",
		"websocketError":    "websocketfout",
		"websocketClosed":   "websocket gesloten",
		"bufferRejected":    "buffer geweigerd",
		"codecRefused":      "codec geweigerd: {codec}",
		"refused":           "geweigerd",
		"streamRefused":     "stream geweigerd",
	},
}

// pages is every language's finished page, rendered once at start.
var pages = i18n.MustRenderPages("webui", page, catalogs)

// pageFor returns the page in one language, falling back to the default for a
// language this build does not carry.
func pageFor(language i18n.Language) string {
	if rendered, ok := pages[language]; ok {
		return rendered
	}
	return pages[i18n.Default]
}

package pairing

import "github.com/local/motorola-vm65-bridge/internal/i18n"

// Every string the pairing page shows lives here. The mechanism — %%token%% in
// the markup, the same catalog handed to the script as a dictionary — is
// described in internal/i18n/page.go.
//
// The handlers' own refusals are in here too, because this page prints what the
// server sends verbatim: a translated page that answers a wrong code in English
// is a page that switches language exactly when it has bad news.
var catalogs = map[i18n.Language]i18n.Catalog{
	i18n.English: {
		"htmlLang": "en",
		"title":    "Pair Motorola Nursery Homeassistant Bridge",

		"heading": "Pair your Motorola Nursery account",
		"lede": "Motorola emails a one-time code. Nothing here is stored in the add-on " +
			"configuration, and the add-on does not need a restart.",
		"emailLabel":    "Account email",
		"emailHint":     "The address you use in the Motorola Nursery app.",
		"sendCode":      "Send code",
		"codeLabel":     "Code from the email",
		"finishPairing": "Finish pairing",
		"resend":        "Send a new code",
		"pairedTitle":   "Paired.",
		"pairedBody": "The add-on is starting your cameras now. This page becomes the stream " +
			"view once they are up — give it a moment, then reload.",
		"stepOne":   "Enter the email address of your Motorola Nursery account.",
		"stepTwo":   "Open the email from Motorola and copy the code.",
		"stepThree": "Paste it here. Codes expire, so request a new one if it is refused.",

		"sentToOne":      "Sent to {email}. Valid for about {minutes} more minute.",
		"sentToMany":     "Sent to {email}. Valid for about {minutes} more minutes.",
		"enterCode":      "Enter the code from the email.",
		"enterEmail":     "Enter the email address of your Motorola account.",
		"codeSent":       "Code sent. It can take a minute to arrive.",
		"newCodeSent":    "A new code is on its way.",
		"somethingWrong": "Something went wrong.",
		"noAnswer":       "The add-on did not answer. Is it still running?",
		"unreachable":    "Could not reach the add-on.",

		"errState":       "Could not read the pairing state.",
		"errRequestCode": "Could not request a code. Check the address and try again.",
		"errCodeWrong":   "That code was not accepted. Check it and try again.",
		"errCodeExpired": "That code has expired. Request a new one.",
		"errBadRequest":  "The request could not be read.",
	},
	i18n.Dutch: {
		"htmlLang": "nl",
		"title":    "Motorola Nursery Homeassistant Bridge koppelen",

		"heading": "Koppel je Motorola Nursery-account",
		"lede": "Motorola mailt je een eenmalige code. Niets hiervan wordt in de configuratie " +
			"van de add-on bewaard, en de add-on hoeft niet opnieuw te starten.",
		"emailLabel":    "E-mailadres van het account",
		"emailHint":     "Het adres dat je in de Motorola Nursery-app gebruikt.",
		"sendCode":      "Code versturen",
		"codeLabel":     "Code uit de e-mail",
		"finishPairing": "Koppelen afronden",
		"resend":        "Nieuwe code sturen",
		"pairedTitle":   "Gekoppeld.",
		"pairedBody": "De add-on start nu je camera's. Deze pagina wordt het camerabeeld zodra " +
			"ze draaien — geef het even de tijd en herlaad de pagina.",
		"stepOne":   "Vul het e-mailadres van je Motorola Nursery-account in.",
		"stepTwo":   "Open de e-mail van Motorola en kopieer de code.",
		"stepThree": "Plak hem hier. Codes verlopen, dus vraag een nieuwe aan als hij wordt geweigerd.",

		"sentToOne":      "Verstuurd naar {email}. Nog ongeveer {minutes} minuut geldig.",
		"sentToMany":     "Verstuurd naar {email}. Nog ongeveer {minutes} minuten geldig.",
		"enterCode":      "Vul de code uit de e-mail in.",
		"enterEmail":     "Vul het e-mailadres van je Motorola-account in.",
		"codeSent":       "Code verstuurd. Hij kan een minuut onderweg zijn.",
		"newCodeSent":    "Er is een nieuwe code onderweg.",
		"somethingWrong": "Er ging iets mis.",
		"noAnswer":       "De add-on gaf geen antwoord. Draait hij nog?",
		"unreachable":    "De add-on is niet bereikbaar.",

		"errState":       "De koppelstatus kon niet worden gelezen.",
		"errRequestCode": "Er kon geen code worden aangevraagd. Controleer het adres en probeer het opnieuw.",
		"errCodeWrong":   "Die code is niet geaccepteerd. Controleer hem en probeer het opnieuw.",
		"errCodeExpired": "Die code is verlopen. Vraag een nieuwe aan.",
		"errBadRequest":  "Het verzoek kon niet worden gelezen.",
	},
}

// pages is every language's finished page, rendered once at start.
var pages = i18n.MustRenderPages("pairing", page, catalogs)

// pageFor returns the page in one language, falling back to the default for a
// language this build does not carry.
func pageFor(language i18n.Language) string {
	if rendered, ok := pages[language]; ok {
		return rendered
	}
	return pages[i18n.Default]
}

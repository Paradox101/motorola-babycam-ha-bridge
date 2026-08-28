# Runtime protocol capture

Onderzoeksinstrument voor één normale sessie van het eigen account/apparaat. De standaardcapture bewaart geen ruwe hex en redigeert e-mailadressen, bekende tokenvelden, OTP/loginset-geheimen en lange opaque waarden.

```powershell
.\tools\runtime-protocol-capture\run-capture.ps1 -Mode Attach
```

Correlatie na afloop:

```powershell
python .\tools\runtime-protocol-capture\correlate.py `
  .\runtime-logs\protocol-capture.log `
  .\runtime-logs\protocol-correlated.jsonl
```

Beperking: de ARMv7 Magic-library draait in de x86_64-emulator via Android native translation. Haar interne libc-calls zijn mogelijk niet zichtbaar vanuit het x86 Frida-proces. Een emulator-pcap blijft daarom nodig voor Magic-wirebytes als deze hooks alleen de controlroute zien.


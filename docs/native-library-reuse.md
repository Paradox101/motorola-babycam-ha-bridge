# Native Magic library reuse assessment

## Feiten

`libdevconn.so` is een ARMv7 Android shared object (`ELF32`, `EM_ARM`, Bionic ABI). Dependencies: Android `liblog.so`, plus `libm.so`, `libdl.so` en `libc.so`. Er is geen JNI-export; de app gebruikt C-ABI via Dart FFI.

## Technische betekenis

De afwezigheid van JNI maakt een kleine native wrapper conceptueel mogelijk op ARMv7. De aanwezige binary is echter niet rechtstreeks bruikbaar voor de vereiste add-onarchitecturen:

- amd64 en aarch64 kunnen ARMv7-machinecode niet native laden;
- Android/Bionic ABI verschilt van glibc/musl;
- `liblog.so` moet worden geshimd;
- callbacks, threading en socketgedrag moeten nog worden getest;
- de redistributie-/licentiestatus is niet vastgesteld.

## Conclusie

Libraryhergebruik is een onderzoeksfallback, geen productbasis. Een ARM-emulatielaag of verborgen Android-runtime zou het projectdoel schenden. Alleen wanneer een juridisch bruikbare aarch64/amd64-build wordt gevonden of broncompatibiliteit aantoonbaar is, kan een compatibility shim opnieuw worden beoordeeld. De primaire route blijft een native Go-herimplementatie op basis van bewezen wiregedrag.


# Maestro flows

Five flows, one per destination: Home, Search, Library, Offline and You. Each
one launches the app, taps the destination in the bottom bar and asserts text
that only that destination shows.

Run against a debug build and a device that has already completed setup, because
the flows do not register a server. A fresh install stays on `Connect to syncd`
and has no bottom bar.

```sh
maestro test .maestro
```

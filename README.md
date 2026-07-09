# goscape-singleplayer

A single-binary, singleplayer RuneScape: the [goscape](../goscape) server stack
and the [goscape-client](../goscape-client) game client combined into one
process, talking over loopback TCP.

## Branch model

`main` holds only this README. Code lives on revision branches (`rev-274`,
`rev-254`, `rev-245.2`, `rev-244`, `rev-225`), each pairing with the
same-named branches of `goscape` and `goscape-client`.

## Dependency wiring

Neither `goscape` nor `goscape-client` is fetchable as a Go module, so each
revision branch uses `replace` directives pointing at sibling checkouts
(`../goscape`, `../goscape-client`). Building a given revision requires those
checkouts to have the matching revision branch checked out.

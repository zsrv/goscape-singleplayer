# Security policy

## Reporting a vulnerability

Report privately through GitHub: **Security → Report a vulnerability** on this
repository. Please do not open a public issue for anything exploitable.

Include the revision branch you are on (`rev-225` … `rev-274`).

If the problem is in the game protocol, the world server, or the client's
handling of cache data, it almost certainly belongs upstream instead — see
[Scope](#scope).

## Supported branches

Each `rev-N` branch pairs with the same-named branches of
[goscape](https://github.com/zsrv/goscape) and
[goscape-client](https://github.com/zsrv/goscape-client), pinned by exact
version in `go.mod`. All of them are maintained. `main` carries documentation
only.

## Trust model

**This is a singleplayer launcher. It assumes one trusted user on one machine.**

As shipped, there are **no listeners at all**: world, login, friends and
ondemand all talk over in-memory transports inside one process, so there is
no socket for anything on the network — or on the same machine — to reach.

`--expose-tcp` switches every module to loopback TCP instead. In that mode,
every listener binds `127.0.0.1`, and there is no flag to change that — the
port flags (`--world-port`, `--ondemand-port`, `--login-port`,
`--friends-port`) move the ports, never the bind address.

Under `--expose-tcp`, these are the listeners:

| Listener | Default port | Purpose |
|---|---|---|
| world TCP | 43594 | the game connection, loopback |
| ondemand HTTP | 8080 | serves the packed cache to the in-process client |
| login gRPC | 2004 | internal; account lookup and player saves |
| friends gRPC | 2005 | internal; friends and private messages |

What that model does **not** cover, and **only under `--expose-tcp`**:

- **Other local users.** `login` and `friends` speak gRPC with no
  authentication and no transport security. On a shared or multi-user machine,
  any local account that can open a loopback socket can read and modify your
  characters, saves and friends data. `login` also registers gRPC server
  reflection, so its schema is discoverable. In the default configuration
  there is no socket for another local account to reach, so this risk does
  not apply — it exists only once `--expose-tcp` opens the loopback ports.
- **Port forwarding.** Anything that republishes a loopback port — an SSH
  tunnel, a container port mapping, a VPN or debugging proxy — removes the only
  control keeping those two services private. This applies only under
  `--expose-tcp`; with no listener there is no port to republish.

If you want to run goscape for more than one player, run the real server
instead of this launcher, and read
[goscape's SECURITY.md](https://github.com/zsrv/goscape/blob/main/SECURITY.md)
first.

## Known weak defaults

These are inherited from the embedded server and are deliberate for
singleplayer use:

- **Accounts auto-register.** Any username and password typed at the login
  screen creates that character on first login. Passwords are bcrypt-hashed
  into the world database, but nothing verifies who you are — the account is
  whoever types the name first.
- **The built-in login RSA key is public.** goscape compiles in a 512-bit key
  whose private exponent is in its repository, because the matching public key
  is baked into the stock client. It offers no confidentiality here; over
  loopback that does not matter.
- **`--data-dir` is unencrypted.** The world database (`goscape.db`), player
  saves under `players/`, and the ondemand `public/` directory are plain files
  with default permissions. Back them up like save files, not like secrets.

## Scope

In scope: this repository's own code — the process lifecycle in
`cmd/goscape-singleplayer`, the embedded-server wiring in `internal/server`,
and the pins in `go.mod`. A flaw that widens the trust model above — a socket
being opened at all in the default configuration, a listener that ends up on
a non-loopback address under `--expose-tcp`, a shutdown path that corrupts
saves — is a bug here.

Out of scope, and better reported upstream:

- the RS2 protocol, RuneScript execution, the world/login/friends/ondemand
  modules → [goscape](https://github.com/zsrv/goscape/security)
- client-side cache parsing, rendering, input handling →
  [goscape-client](https://github.com/zsrv/goscape-client/security)

Exposing a loopback port to a network you do not control is a deployment
choice, not a vulnerability in this code.

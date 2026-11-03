# Dependency update — 2026-11-03

The `v2.1` branch now uses Go 1.27, DTLS 3.1.15, TURN 5.1.3,
KCP 5.7.2 and smux 1.5.58, with updated HTTP, TLS and crypto dependencies.
The client TURN network adapter implements the transport/v5 interface required
by TURN 5.1.3. Server DTLS packet handling still uses transport/v4 where required
by its upstream dependency. Wire formats, WRAP keys and session negotiation
were not changed.

CI no longer reports the three unused browser-profile helpers left after
Telemost removal. Both workflows read the compiler version from `go.mod`.
golangci-lint 2.15.0 is installed from source so its compiler matches the module.

## Verification

- `go test -race ./...`, golangci-lint and `git diff --check` passed.
- Client and server builds passed for Linux amd64, macOS arm64, Android arm64
  and Android arm (API 21, NDK 29). The Linux arm64 server also built.
- A loopback UDP backend verified ordinary datagram round trips using unchanged
  WRAP code and dependency versions from iOS build 471, the previous Android
  client (DTLS 3.0.10 / TURN 5.0.2), and Android 1.2.3
  (DTLS 3.1.15 / TURN 5.1.3).
- The same clients passed TURN allocation, WRAP, DTLS and health-echo checks
  through actual VK relays over both TCP and UDP against an isolated canary,
  then against the updated production listeners on Dragon-RV and Noah-FV.
  One initial VK UDP allocation timed out; the complete follow-up matrix passed.

These checks exercise actual client transport code; they are not an interactive
test on a physical iPhone or Android device.

## Production

Both nodes run `local/rabbithole-turn-proxy:20261103-go127`. The static server
binary has SHA-256:

```text
fb4d4a9ee3f74038828c6454d991e02e18b0fa57a247c6d5ed19abe1fc14ec84
```

The existing Alpine 3.23 runtime layout, unprivileged user, command arguments,
WRAP keys, UDP port 56010, private metrics binding, backend and configured
resource limits were preserved. Readiness passed after each node was updated.
The temporary canary was removed. Existing sessions may reconnect during a
container replacement.

The previous image, `local/rabbithole-turn-proxy:da95f03-observability`, remains
available for rollback. Change only the service's image back to that tag and run
`docker compose up -d --no-deps <service>` in its deployment directory.
Dragon-RV's `.deploy/Dockerfile` also names the updated binary; restore its COPY
source to `rabbithole-turn-proxy-server.da95f03` before rebuilding the old tag.

Deployment directories and service names:

| Node | Directory | Service |
| --- | --- | --- |
| Dragon-RV | `/home/container/docker/rabbithole-turn-proxy` | `rabbithole-turn-proxy` |
| Noah-FV | `/home/container/docker/vk-turn-proxy-v1-wrap` | `vk-turn-proxy-v1-wrap` |

Secrets and temporary relay credentials were excluded from this repository.

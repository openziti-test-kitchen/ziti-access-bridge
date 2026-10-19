# Ziti Access Bridge

Ziti Access Bridge (ZAB) carries Cloudflare Access traffic onto an OpenZiti overlay network.

Cloudflare Access owns the public hostname, the identity provider login, and the access
policy. ZAB owns the last hop. It runs an embedded Cloudflare Tunnel, verifies the Cloudflare
Access JWT on every request, authenticates the requester to OpenZiti with that same JWT
through an External JWT Signer, and proxies the request over the OpenZiti service that serves
the requested hostname and path. Every request travels as the original requester, mapped to
an OpenZiti identity. Service Policy, logging, and metrics are therefore all per identity.
ZAB's own OpenZiti identity
trusts and discovers the controllers and administers the ZAB config types. It does not carry
service traffic.


## Build

```
go build ./cmd/zab
```

## Configure

ZAB reads `config.yml` from the working directory, or the file named by `--config`. Start
from `etc/example.config.yml`, which documents every setting. Relative paths in the file are
relative to the file.

## Enroll

Create an identity for ZAB on the OpenZiti controller and enroll with its JWT:

```
zab enroll zab.jwt
```

Enrollment generates a key, obtains the certificate and CA bundle, writes them to the files
the `identity` section names, and records the controller addresses in the endpoint file.
The endpoint file is kept current on every connection as controllers are added or removed.

## Run: Cloudflare Access to the demo server

Browser, Cloudflare Access, Cloudflare Tunnel, ZAB, OpenZiti, demo-http-server, on one
machine with a quickstart network. Replace `<zone>`, `<team>`, `<aud>`, and `<email>`.

### 1. Build

```
go build -o bin/ ./cmd/zab ./demo/cmd/demo-http-server
```

### 2. Cloudflare

In the Cloudflare dashboard:

1. Zero Trust, Networks, Tunnels: create a named tunnel. Copy its token.
2. On that tunnel, add a public hostname `demo.<zone>`. The service field does not matter.
3. Zero Trust, Access, Applications: add a self-hosted application on `demo.<zone>` with a
   policy allowing `<email>`. Copy the application's AUD tag and your team domain,
   `<team>.cloudflareaccess.com`.

### 3. OpenZiti network

```
ziti edge quickstart
```

In a second shell:

```
ziti edge login localhost:1280 -u admin -p admin -y
ziti edge create identity zab --admin -o zab.jwt
ziti edge create identity me --external-id <email>
ziti edge create ext-jwt-signer cloudflare-access https://<team>.cloudflareaccess.com --jwks-endpoint https://<team>.cloudflareaccess.com/cdn-cgi/access/certs --audience <aud> --claims-property email
```

The last command prints the signer's id. The auth policy takes the id, not the name:

```
ziti edge update auth-policy default --primary-ext-jwt-allowed --primary-ext-jwt-allowed-signers <signer id>
```

### 4. ZAB

Copy `etc/example.config.yml` to `etc/config.yml` and set `cloudflare.teamDomain`,
`cloudflare.audTags`, and `cloudflare.tunnelTokenFile`. Put the tunnel token in that file.

```
bin/zab --config etc/config.yml enroll zab.jwt
bin/zab --config etc/config.yml agent register-configs
```

### 5. The service

```
bin/zab --config etc/config.yml agent service set demo --route demo.<zone>
ziti edge create config demo.host host.v1 '{"protocol":"tcp","address":"127.0.0.1","port":28080}'
ziti edge update service demo --configs demo.zab-v1.0.0,demo.host
ziti edge create service-policy demo-dial Dial --service-roles '@demo' --identity-roles '#all'
ziti edge create service-policy demo-bind Bind --service-roles '@demo' --identity-roles '#all'
```

The quickstart router hosts `demo` from the host config. No separate tunneler is needed.
`update service --configs` replaces the list, so name every config the service keeps.

### 6. Run

Two shells:

```
bin/demo-http-server --port 28080 --cloudflare-team-domain <team>.cloudflareaccess.com --cloudflare-aud <aud>
bin/zab --config etc/config.yml run
```

### 7. Try it

Open `https://demo.<zone>`. After the Cloudflare Access login the page greets you by email
and shows the verified JWT.

```
bin/zab --config etc/config.yml agent status
curl http://127.0.0.1:28080/echo
```

`status` shows the controller live, the running ZAB, and `zab-v1.0.0 - supported`. The
`curl` returns 401: nothing reaches the demo server without passing Cloudflare Access.

## Agent commands

A running ZAB listens on the unix socket named by `socketFile`. From another shell:

```
zab agent status             # enrollment, controller liveness, the running zab, config types
zab agent register-configs   # register or update the zab config types on the controller
zab agent service set app --route app.example.com
zab agent service set api --route app.example.com/api --strip-path-prefix --scheme https
zab agent service show app
zab agent service list
zab agent stop
```

`service set` attaches a zab config to a service and creates the service bare when it does
not exist. Policies, other configs, and deletion stay with the OpenZiti CLI.
`register-configs`, the `service` commands, and the config type section of `status` use
ZAB's own identity against the Edge Management API, so that identity needs admin rights for
them. Running the bridge does not.

## Packages

| Package     | Purpose                                                        |
|-------------|----------------------------------------------------------------|
| `zab`       | `Bridge`, the `http.Handler`, and `Run`, the whole program     |
| `agent`     | Status reporting and config type registration for `zab agent` |
| `cfaccess`  | Cloudflare Access JWT verification against the team JWKS       |
| `cftunnel`  | Embedded Cloudflare Tunnel delivering requests to a handler    |
| `config`    | The configuration file                                         |
| `control`   | The control socket a running ZAB serves and `zab agent` uses   |
| `endpoints` | The endpoint file: controller addresses, kept current          |
| `enroll`    | Enrolling ZAB's OpenZiti identity from an enrollment JWT       |
| `metrics`   | The Prometheus `/metrics` listener                             |
| `route`     | The versioned `zab-v*` config types and hostname plus path prefix routing |
| `sdkpool`   | One OpenZiti SDK context per requester                         |
| `demo`      | `demo-http-server`, the far end of the walkthrough above       |

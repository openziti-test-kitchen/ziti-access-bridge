# ZAB demo

The end-to-end walkthrough is in the repository README under "Run: Cloudflare Access to the
demo server". This folder holds the far end of it.

```
demo/cmd/demo-http-server   a page that greets the verified requester and shows the JWT, plus a JSON echo at /echo
```

## demo-http-server

```
demo-http-server --port 28080 --cloudflare-team-domain <team>.cloudflareaccess.com --cloudflare-aud <aud>
```

1. `/` is a dark themed page. Without a valid Cloudflare Access JWT it shows an error with
   401. With one it greets the requester by email with the time in the browser's time zone,
   confirms the JWT was found and verified, and shows the JWT beside its decoded header and
   payload.
2. `/echo` returns the request as JSON: method, path, host, remote address, headers, and the
   verified claims.
3. `/healthz` answers 200 and skips verification.

Without `--cloudflare-team-domain` and `--cloudflare-aud` the server decodes the JWT without
verifying it and says so on the page. `--tls-cert` and `--tls-key` serve HTTPS, for the
`scheme: https` and `tlsVerify` settings of a zab config. `--interface` picks the listen
address, default `127.0.0.1`.

# tsnsrv - an experimental tailnet reverse proxy

This package includes a little go program that sets up a reverse proxy
listening on your tailnet (optionally with a funnel), forwarding
requests to a service reachable from the machine running this
program. This is directly and extremely inspired by the [amazing talk
by Xe Iaso](https://tailscale.dev/blog/tsup-tsnet) about the wonderful
things one can do with
[`tsnet`](https://pkg.go.dev/tailscale.com/tsnet).

## Why use this?

First, you'll want to watch the talk linked above. But if you still
have that question: Say you run a service that you haven't written
yourself (we can't all be as wildly productive as Xe), but you'd still
like to benefit from tailscale's access control, encrypted
communication and automatic HTTPS provisioning? Then you can just run
that service, have it listen on localhost or a unix domain socket,
then run `tsnsrv` and have that expose the service on your tailnet
(or, as I mentioned, on the funnel).

## Is this probably full of horrible bugs that will make you less secure?

Almost certainly:

* I have not thought about request forgery yet

* You're by definition forwarding requests of one degree of
  trustedness to a thing of another degree of trustedness.

Soooo, if it breaks, you get to keep both parts (see "experimental").

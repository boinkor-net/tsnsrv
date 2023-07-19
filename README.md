# tsnsrv - a reverse proxy on your tailnet

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
communication and automatic HTTPS cert provisioning? Then you can just
run that service, have it listen on localhost or a unix domain socket,
then run `tsnsrv` and have that expose the service on your tailnet
(or, as I mentioned, on the funnel).

### Is this probably full of horrible bugs that will make you less secure or more unhappy?

Almost certainly:

* I have not thought much request forgery.

* You're by definition forwarding requests of one degree of
  trustedness to a thing of another degree of trustedness.

* This tool uses go's `httputil.ReverseProxy`, which seems notorious
  for having bugs in its mildly overly-naive URL path rewriting
  (especially resulting in an extraneous `/` getting appended to the
  destination URL path).

## So how do you use this?

First, you have to have a service you want to proxy to, reachable from
the machine that runs tsnsrv. I'll assume it serves plaintext HTTP on
`127.0.0.1:8000`, but it could be on any address, reachable over ipv4
or v6. Assume the service is called `happy-computer`.

Then, you have options:

* Expose the service on your tailnet (and only your tailnet):
  `tsnsrv -name happy-computer http://127.0.0.1:8000`

* Expose the entire service on your tailnet and on the internet:
  `tsnsrv -name happy-computer -funnel http://127.0.0.1:8000`

### Access control to public funnel endpoints

Now, running a whole service on the internet doesn't feel great
(especially if the authentication/authorization story depended on it
being reachable only on your tailnet); you might want to expose only a
webhook endpoint from a separate tsnsrv invocation, that allows access
only to one or a few subpaths. Assuming you want to run a matrix
server:

```sh
tsnsrv -name happy-computer-webhook -funnel -stripPrefix=false -prefix /_matrix -prefix /_synapse/client http://127.0.0.1:8000
```

Each `-prefix` flag adds a path to the list of URLs that external
clients can see (Anything outside that list returns a 404).

The `-stripPrefix` flag tells tsnsrv to leave the prefix intact: By default, it strips off the matched portion, so that you can run it with:
`tsnsrv -name hydra-webhook -funnel -prefix /api/push-github http://127.0.0.1:3001/api/push-github`
which would be identical to
`tsnsrv -name hydra-webhook -funnel -prefix /api/push-github -stripPrefix=false http://127.0.0.1:3001`

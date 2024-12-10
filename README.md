# k8speer

Watch a service, get notified of its members. 

## usage

Can watch any service, but my motivating use case is for a k8s deployment and it's related service to be able to have each pods in the deployment track who else is there.

## example

Let's say you want to watch peers in your service, say that you want to maintain a groupcache peer list.

```go
// in a goroutine
err := k8speer.WatchPeers(ctx, ll, clientset, serviceName, namespace, groupcachePort, func(peers []k8speer.Peer) {
  out := make([]galaxycache.Peer, 0, len(peers))
  for _, p := range peers {
    out = append(out, galaxycache.Peer{ID: p.ID, URI: p.URI})
  }
  universe.SetPeers(out...)
})
```

If you were using `venmo/galaxycache` it would look like:

```go
import gchttp "github.com/vimeo/galaxycache/http"

gcl, err := net.Listen("tcp", net.JoinHostPort(host, groupcachePort))
if err != nil {
  return errors.Wrap(err, "groupcache listening on host/port")
}
defer gcl.Close()

var protocol galaxycache.FetchProtocol
protocol = gchttp.NewHTTPFetchProtocol(&gchttp.HTTPOptions{})

selfURI := net.JoinHostPort(groupcacheSelf, groupcachePort)
universe := galaxycache.NewUniverse(
  protocol,
  selfURI,
)
defer func() {
  if err := universe.Shutdown(); err != nil {
    // handle err
  }
}()
cacheMux := http.NewServeMux()
gchttp.RegisterHTTPHandler(universe, nil, cacheMux)
```

## license

MIT

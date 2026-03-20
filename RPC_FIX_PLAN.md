# Cap'n Proto RPC Fix Plan

## Problem
Multiple RPC servers were creating new `rpc.Conn` instances on every `Client()` call, wrapping the same `net.Pipe()` transport. This causes message corruption as multiple connections try to read from the same stream.

## Solution Pattern Applied

### For In-Process Only Servers
Services that only need local communication:
```go
func (s *Server) Client(clientID string) capnp.Client {
    return capnp.Client(ServiceType_ServerToClient(s))
}
```

### For Distributed Servers  
Services that support both local AND remote RPC:
- Create server and client connections ONCE in constructor
- Both wrap the same bidirectional transport (`net.Pipe()`)
- Cache and reuse the single client connection
- Return `clientConn.Bootstrap(ctx)` from `Client()`

## Servers Fixed

1. ✅ **GraphServer** - Already correct (in-process only)
2. ✅ **TokenizerServer** - Fixed to distributed pattern
3. ✅ **PrompterServer** - Fixed to distributed pattern  
4. ✅ **ForestServer (DMT)** - Fixed to distributed pattern
5. ✅ **HASServer** - Already correct (in-process only)
6. ✅ **MacroIndexServer** - Fixed to distributed pattern
7. ✅ **ReaderServer** - Already correct (in-process only)

## Status

All RPC servers now follow the correct Cap'n Proto pattern from the official documentation.
The isolated RPC test passes, but the full integration test still fails with the same error.
This suggests there may be an additional issue beyond the RPC connection pattern, possibly:
- Race condition in server initialization
- Issue with how errnie.Guard handles Cap'n Proto panics
- Problem in the Cap'n Proto alpha version itself
- Context or lifecycle issue

Next step: Deep dive into why `future.Struct()` is failing to decode the RPC result message.
